package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/workflow-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/workflow-service/internal/config"
	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
	"github.com/arda-labs/arda/apps/workflow-service/internal/migration"
	"github.com/arda-labs/arda/apps/workflow-service/internal/notificationclient"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/workflow-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/workflow-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/workflow-service/internal/worker"
	capitalclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/capital"
	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	depositclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/deposit"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	hrmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/hrm"
	iamclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/iam"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	statisticalclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/statistical"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	// Database Connection
	db, err := sql.Open("pgx/v5", cfg.DatabaseDSN)
	if err != nil {
		logger.Error("Failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	ardapostgres.ConfigureDefaultPool(db, logger)

	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("Failed to ping database", "err", err)
		os.Exit(1)
	}

	if err := migration.Run(db, "postgres"); err != nil {
		logger.Error("Failed to run migrations", "err", err)
		os.Exit(1)
	}
	logger.Info("Database migrations applied successfully")

	// Zeebe Client
	zeebeSvc, err := service.NewZeebeService(cfg.ZeebeAddr)
	if err != nil {
		logger.Error("Failed to connect to Zeebe gateway", "err", err)
		os.Exit(1)
	}
	defer zeebeSvc.Close()

	// Repositories
	mappingRepo := repository.NewMappingRepository(db)
	caseRepo := repository.NewCaseRepository(db)
	processDefinitionRepo := repository.NewProcessDefinitionRepository(db)

	for _, process := range bootstrap.BuiltInProcesses() {
		key, err := zeebeSvc.DeployWorkflow(context.Background(), process.ResourceName, process.Content)
		if err != nil {
			logger.Error("Failed to deploy built-in workflow", "resource", process.ResourceName, "err", err)
			os.Exit(1)
		}
		if _, err := processDefinitionRepo.UpsertBuiltIn(context.Background(), repository.ProcessDefinitionImport{
			ProcessCode:  process.ProcessCode,
			Name:         process.Name,
			ResourceName: process.ResourceName,
			XMLContent:   string(process.Content),
			Status:       "ACTIVE",
		}, key); err != nil {
			logger.Error("Failed to seed built-in workflow definition", "resource", process.ResourceName, "err", err)
			os.Exit(1)
		}
		logger.Info("Built-in workflow deployed", "resource", process.ResourceName, "processDefinitionKey", key)
	}

	crmClient, err := crmclient.Dial(context.Background(), cfg.CRMGRPCAddr, cfg.AppName, logger)
	if err != nil {
		logger.Error("crm grpc unavailable", "addr", cfg.CRMGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer crmClient.Close()
	logger.Info("crm grpc configured", "addr", cfg.CRMGRPCAddr)

	// Loan client is optional: adjustment workers register only when
	// loan-service is reachable.
	loanClient, loanErr := loanclient.Dial(context.Background(), cfg.LoanGRPCAddr, cfg.AppName, logger)
	if loanErr != nil {
		logger.Warn("loan grpc unavailable — lnm.* workers disabled", "addr", cfg.LoanGRPCAddr, "err", loanErr)
	} else {
		defer loanClient.Close()
		logger.Info("loan grpc configured", "addr", cfg.LoanGRPCAddr)
	}

	iamClient, err := iamclient.Dial(context.Background(), cfg.IAMGRPCAddr, cfg.AppName)
	if err != nil {
		logger.Error("iam grpc unavailable", "addr", cfg.IAMGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer iamClient.Close()
	logger.Info("iam grpc configured", "addr", cfg.IAMGRPCAddr)

	caseRepo.SetIAMClient(&iamAdapter{client: iamClient})

	// CRM adjustment job workers. crm.approve_customer / crm.reject_customer
	// are the terminal service tasks of customer-adjustment-v2.bpmn — both
	// must stay registered or the adjustment case hangs at APPROVE/REJECT.
	crmWorkers := worker.NewCRMWorkers(crmClient, caseRepo)
	crmRequestChangesWorker := zeebeSvc.NewJobWorker("crm.request_customer_changes", crmWorkers.RequestChangesHandler)
	defer crmRequestChangesWorker.Close()
	crmRejectWorker := zeebeSvc.NewJobWorker("crm.reject_customer", crmWorkers.RejectCustomerHandler)
	defer crmRejectWorker.Close()
	crmApproveWorker := zeebeSvc.NewJobWorker("crm.approve_customer", crmWorkers.ApproveCustomerHandler)
	defer crmApproveWorker.Close()
	crmUpdateWorker := zeebeSvc.NewJobWorker("crm.update_customer", crmWorkers.UpdateCustomerHandler)
	defer crmUpdateWorker.Close()
	logger.Info("workflow CRM adjustment job workers registered")

	crmRegisterWorkers := worker.NewCRMRegisterWorkers(crmClient, caseRepo)
	crmRegisterValidateWorker := zeebeSvc.NewJobWorker(worker.JobCRMRegisterValidate, crmRegisterWorkers.ValidateHandler)
	defer crmRegisterValidateWorker.Close()
	crmRegisterExecuteWorker := zeebeSvc.NewJobWorker(worker.JobCRMRegisterExecute, crmRegisterWorkers.ExecuteHandler)
	defer crmRegisterExecuteWorker.Close()
	crmRegisterCancelWorker := zeebeSvc.NewJobWorker(worker.JobCRMRegisterCancel, crmRegisterWorkers.CancelHandler)
	defer crmRegisterCancelWorker.Close()
	logger.Info("workflow CRM register v2 job workers registered")

	restAddr := cfg.ZeebeRestAddr
	if restAddr == "" {
		restAddr = service.DeriveZeebeRestAddr(cfg.ZeebeAddr)
	}
	tasklistAddr := cfg.ZeebeTasklistAddr
	if tasklistAddr == "" {
		tasklistAddr = service.DeriveZeebeTasklistAddr(restAddr)
	}
	esURL := cfg.ZeebeESURL
	if esURL == "" {
		esURL = service.DeriveZeebeESAddr(cfg.ZeebeAddr)
	}
	esIndex := service.NewZeebeUserTaskIndex(esURL)
	zeebeRest := service.NewZeebeRestClient(restAddr, tasklistAddr, esIndex)
	if zeebeRest != nil && zeebeRest.Enabled() {
		if esIndex != nil && esIndex.Enabled() {
			logger.Info("zeebe REST + elasticsearch user task index configured", "restAddr", restAddr, "esURL", esURL)
		} else {
			logger.Warn("zeebe REST configured but ZEEBE_ES_URL missing — v2 native user tasks cannot be discovered")
		}
	} else {
		logger.Warn("zeebe REST client not configured — native user tasks (v2) require ZEEBE_REST_ADDR")
	}

	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer syncCancel()
	assignmentResolver := service.NewAssignmentResolver(caseRepo)
	if projector := worker.NewUserTaskProjector(zeebeRest, caseRepo, assignmentResolver); projector != nil {
		go projector.Run(syncCtx)
	}

	notificationWorkers := worker.NewNotificationWorkers()
	notificationEmailWorker := zeebeSvc.NewJobWorker("notification.email", notificationWorkers.SendEmailHandler)
	defer notificationEmailWorker.Close()
	notificationSMSWorker := zeebeSvc.NewJobWorker("notification.sms", notificationWorkers.SendSMSHandler)
	defer notificationSMSWorker.Close()
	notificationPushWorker := zeebeSvc.NewJobWorker("notification.push", notificationWorkers.SendPushHandler)
	defer notificationPushWorker.Close()
	notificationCustomerResultWorker := zeebeSvc.NewJobWorker("notification.customer_registration_result", notificationWorkers.CustomerRegistrationResultHandler)
	defer notificationCustomerResultWorker.Close()
	logger.Info("workflow notification job workers registered")

	var hrmClient *hrmclient.Client
	if cfg.HRMGRPCAddr != "" {
		hc, err := hrmclient.Dial(context.Background(), cfg.HRMGRPCAddr, cfg.AppName, logger)
		if err != nil {
			logger.Error("hrm grpc dial", "err", err)
			os.Exit(1)
		}
		defer hc.Close()
		hrmClient = hc
	}

	// Finance client is optional: the disbursement register/complete legs and
	// the manual posting (fin.*) workers register only when finance-service
	// is reachable.
	financeClient, financeErr := financeclient.Dial(context.Background(), cfg.FinanceGRPCAddr, cfg.AppName, logger)
	if financeErr != nil {
		logger.Warn("finance grpc unavailable — lnm.disb-* and fin.* posting workers disabled", "addr", cfg.FinanceGRPCAddr, "err", financeErr)
	} else {
		defer financeClient.Close()
		logger.Info("finance grpc configured", "addr", cfg.FinanceGRPCAddr)
	}

	if loanErr == nil {
		loanWorkers := worker.NewLoanWorkers(loanClient, caseRepo)
		for _, kind := range loanclient.Kinds {
			validateH, executeH, cancelH := loanWorkers.Handlers(kind)
			v := zeebeSvc.NewJobWorker("lnm."+kind+".validate", validateH)
			e := zeebeSvc.NewJobWorker("lnm."+kind+".execute", executeH)
			c := zeebeSvc.NewJobWorker("lnm."+kind+".cancel", cancelH)
			defer v.Close()
			defer e.Close()
			defer c.Close()
		}
		logger.Info("workflow loan adjustment workers registered", "kinds", len(loanclient.Kinds))

		// General provision (LNM.307.01): the maker submits a per-org period,
		// the checker resolution recomputes + posts via the LNM_PROVISION
		// rule card inside loan-service.
		gpWorkers := worker.NewGeneralProvisionWorkers(loanClient, caseRepo)
		gpv, gpe, gpc := gpWorkers.Handlers()
		gpvw := zeebeSvc.NewJobWorker("lnm.general-provision.validate", gpv)
		gpew := zeebeSvc.NewJobWorker("lnm.general-provision.execute", gpe)
		gpcw := zeebeSvc.NewJobWorker("lnm.general-provision.cancel", gpc)
		defer gpvw.Close()
		defer gpew.Close()
		defer gpcw.Close()
		logger.Info("workflow general provision workers registered")

		// Loan formation (LOAN_FORMATION_V2, EPAS LNM.201.01): validate reads
		// the contract state, execute activates the contract, cancel rejects
		// it. No finance involvement — formation moves no money.
		formationWorkers := worker.NewFormationWorkers(loanClient, caseRepo)
		fv, fe, fc := formationWorkers.Handlers()
		fvw := zeebeSvc.NewJobWorker("lnm.loan.formation.validate", fv)
		few := zeebeSvc.NewJobWorker("lnm.loan.formation.execute", fe)
		fcw := zeebeSvc.NewJobWorker("lnm.loan.formation.cancel", fc)
		defer fvw.Close()
		defer few.Close()
		defer fcw.Close()
		logger.Info("workflow loan formation workers registered")

		// Disbursement two-flow workers (P1b v2): the register leg reserves
		// the posting at init and posts on approve; the complete leg
		// settles the in-transit hold against cash.
		if financeErr == nil {
			disbRegister := worker.NewDisbursementWorkers(worker.RegisterFlow, loanClient, financeClient, caseRepo)
			ri, rv, re, rc := disbRegister.Handlers()
			riw := zeebeSvc.NewJobWorker("lnm.disb-register.init", ri)
			rvw := zeebeSvc.NewJobWorker("lnm.disb-register.validate", rv)
			rew := zeebeSvc.NewJobWorker("lnm.disb-register.execute", re)
			rcw := zeebeSvc.NewJobWorker("lnm.disb-register.cancel", rc)
			defer riw.Close()
			defer rvw.Close()
			defer rew.Close()
			defer rcw.Close()

			disbComplete := worker.NewDisbursementWorkers(worker.CompleteFlow, loanClient, financeClient, caseRepo)
			ciH, cvH, ceH, ccH := disbComplete.Handlers()
			ciw := zeebeSvc.NewJobWorker("lnm.disb-complete.init", ciH)
			cvw := zeebeSvc.NewJobWorker("lnm.disb-complete.validate", cvH)
			cew := zeebeSvc.NewJobWorker("lnm.disb-complete.execute", ceH)
			ccw := zeebeSvc.NewJobWorker("lnm.disb-complete.cancel", ccH)
			defer ciw.Close()
			defer cvw.Close()
			defer cew.Close()
			defer ccw.Close()
			logger.Info("workflow disbursement register/complete workers registered")

			// Collection: one case riding the two-phase finance lifecycle —
			// init reserves the cash hold, validate re-checks + re-reserves,
			// execute posts on approve, cancel releases on reject.
			colWorkers := worker.NewCollectionWorkers(loanClient, financeClient, caseRepo)
			ci, cv, ce, cc := colWorkers.Handlers()
			civ := zeebeSvc.NewJobWorker("lnm.collection.init", ci)
			cvv := zeebeSvc.NewJobWorker("lnm.collection.validate", cv)
			ceE := zeebeSvc.NewJobWorker("lnm.collection.execute", ce)
			ccc := zeebeSvc.NewJobWorker("lnm.collection.cancel", cc)
			defer civ.Close()
			defer cvv.Close()
			defer ceE.Close()
			defer ccc.Close()
			logger.Info("workflow collection workers registered")

			// Batch flows (iteration 13 — 1 hồ sơ — N hợp đồng): same
			// Reserve → Validate → Post / Release lifecycle, one N-line
			// posting per batch, settle loops the per-row semantics in
			// loan-service.
			batchRegister := worker.NewBatchWorkers(worker.BatchDisbRegisterFlow, loanClient, financeClient, caseRepo)
			bri, brv, bre, brc := batchRegister.Handlers()
			briw := zeebeSvc.NewJobWorker("lnm.disb-batch-register.init", bri)
			brvw := zeebeSvc.NewJobWorker("lnm.disb-batch-register.validate", brv)
			brew := zeebeSvc.NewJobWorker("lnm.disb-batch-register.execute", bre)
			brcw := zeebeSvc.NewJobWorker("lnm.disb-batch-register.cancel", brc)
			defer briw.Close()
			defer brvw.Close()
			defer brew.Close()
			defer brcw.Close()

			batchComplete := worker.NewBatchWorkers(worker.BatchDisbCompleteFlow, loanClient, financeClient, caseRepo)
			bci, bcv, bce, bcc := batchComplete.Handlers()
			bciw := zeebeSvc.NewJobWorker("lnm.disb-batch-complete.init", bci)
			bcvw := zeebeSvc.NewJobWorker("lnm.disb-batch-complete.validate", bcv)
			bcew := zeebeSvc.NewJobWorker("lnm.disb-batch-complete.execute", bce)
			bccw := zeebeSvc.NewJobWorker("lnm.disb-batch-complete.cancel", bcc)
			defer bciw.Close()
			defer bcvw.Close()
			defer bcew.Close()
			defer bccw.Close()

			batchCollection := worker.NewBatchWorkers(worker.BatchCollectionFlow, loanClient, financeClient, caseRepo)
			bki, bkv, bke, bkc := batchCollection.Handlers()
			bkiw := zeebeSvc.NewJobWorker("lnm.collection-batch.init", bki)
			bkvw := zeebeSvc.NewJobWorker("lnm.collection-batch.validate", bkv)
			bkew := zeebeSvc.NewJobWorker("lnm.collection-batch.execute", bke)
			bkcw := zeebeSvc.NewJobWorker("lnm.collection-batch.cancel", bkc)
			defer bkiw.Close()
			defer bkvw.Close()
			defer bkew.Close()
			defer bkcw.Close()
			logger.Info("workflow batch disbursement/collection workers registered")
		}

		var depositClient *depositclient.Client
		if cfg.DepositGRPCAddr != "" {
			dc, err := depositclient.Dial(context.Background(), cfg.DepositGRPCAddr, cfg.AppName, logger)
			if err != nil {
				logger.Error("deposit grpc dial", "err", err)
				os.Exit(1)
			}
			defer dc.Close()
			depositClient = dc
		}

		if hrmClient != nil {
			hrmWorkers := worker.NewHRMRegisterWorkers(hrmClient, caseRepo)
			hv, he, hc := hrmWorkers.Handlers()
			hvv := zeebeSvc.NewJobWorker(worker.JobHRMRegisterValidate, hv)
			heE := zeebeSvc.NewJobWorker(worker.JobHRMRegisterExecute, he)
			hcc := zeebeSvc.NewJobWorker(worker.JobHRMRegisterCancel, hc)
			defer hvv.Close()
			defer heE.Close()
			defer hcc.Close()
			logger.Info("workflow hrm registration workers registered")
		}

		if depositClient != nil {
			depWorkers := worker.NewDepositWorkers(depositClient, caseRepo)
			dv, de, dc := depWorkers.Handlers()
			dvv := zeebeSvc.NewJobWorker("dpm.settle.validate", dv)
			dee := zeebeSvc.NewJobWorker("dpm.settle.execute", de)
			dcc := zeebeSvc.NewJobWorker("dpm.settle.cancel", dc)
			defer dvv.Close()
			defer dee.Close()
			defer dcc.Close()

			addWorkers := worker.NewAdditionalDepositWorkers(depositClient, caseRepo)
			av, ae, ac := addWorkers.Handlers()
			avw := zeebeSvc.NewJobWorker("dpm.additional.validate", av)
			aew := zeebeSvc.NewJobWorker("dpm.additional.execute", ae)
			acw := zeebeSvc.NewJobWorker("dpm.additional.cancel", ac)
			defer avw.Close()
			defer aew.Close()
			defer acw.Close()

			productWorkers := worker.NewProductRequestWorkers(depositClient, caseRepo)
			pv, pe, pc := productWorkers.Handlers()
			for _, prefix := range []string{"dpm.product-register", "dpm.product-edit"} {
				pvw := zeebeSvc.NewJobWorker(prefix+".validate", pv)
				pew := zeebeSvc.NewJobWorker(prefix+".execute", pe)
				pcw := zeebeSvc.NewJobWorker(prefix+".cancel", pc)
				defer pvw.Close()
				defer pew.Close()
				defer pcw.Close()
			}

			ibmPlaceWorkers := worker.NewIBMWorkers(depositClient, caseRepo, "PLACE")
			ipv, ipe, ipc := ibmPlaceWorkers.Handlers()
			ipvw := zeebeSvc.NewJobWorker("ibm.place.validate", ipv)
			ipw := zeebeSvc.NewJobWorker("ibm.place.execute", ipe)
			ipcw := zeebeSvc.NewJobWorker("ibm.place.cancel", ipc)
			defer ipvw.Close()
			defer ipw.Close()
			defer ipcw.Close()

			ibmMovementWorkers := worker.NewIBMWorkers(depositClient, caseRepo, "")
			imv, ime, imc := ibmMovementWorkers.Handlers()
			imvw := zeebeSvc.NewJobWorker("ibm.movement.validate", imv)
			imw := zeebeSvc.NewJobWorker("ibm.movement.execute", ime)
			imcw := zeebeSvc.NewJobWorker("ibm.movement.cancel", imc)
			defer imvw.Close()
			defer imw.Close()
			defer imcw.Close()
			logger.Info("workflow deposit workers registered")
		}
	}

	// Manual posting two-flow workers (FAC-native bút toán lẻ / bút toán
	// kép / ngoại bảng / kết chuyển thu chi): the case variables carry the
	// FE-submitted posting; init reserves, validate re-checks, execute posts
	// on approve, cancel releases on reject. Registered only when
	// finance-service is reachable.
	if financeClient != nil {
		for _, flow := range []worker.ManualPostingFlow{worker.SingleEntryFlow, worker.DoubleEntryFlow, worker.OffBalanceFlow, worker.ClosingFlow} {
			manualPosting := worker.NewManualPostingWorkers(flow, financeClient, caseRepo)
			mi, mv, me, mc := manualPosting.Handlers()
			miw := zeebeSvc.NewJobWorker(flow.TopicPrefix+".init", mi)
			mvw := zeebeSvc.NewJobWorker(flow.TopicPrefix+".validate", mv)
			mew := zeebeSvc.NewJobWorker(flow.TopicPrefix+".execute", me)
			mcw := zeebeSvc.NewJobWorker(flow.TopicPrefix+".cancel", mc)
			defer miw.Close()
			defer mvw.Close()
			defer mew.Close()
			defer mcw.Close()
		}
		logger.Info("workflow manual posting workers registered")

		// Transaction cancellation (hủy giao dịch): no posting request —
		// init/validate guard the referenced POSTED entry via
		// GetJournalEntry, execute reverses it (FIN_TXN_CANCEL) on approve.
		cancellation := worker.NewCancellationWorkers(worker.TxnCancelFlow, financeClient, caseRepo)
		ci, cv, ce, cc := cancellation.Handlers()
		ciw := zeebeSvc.NewJobWorker(worker.TxnCancelFlow.TopicPrefix+".init", ci)
		cvw := zeebeSvc.NewJobWorker(worker.TxnCancelFlow.TopicPrefix+".validate", cv)
		cew := zeebeSvc.NewJobWorker(worker.TxnCancelFlow.TopicPrefix+".execute", ce)
		ccw := zeebeSvc.NewJobWorker(worker.TxnCancelFlow.TopicPrefix+".cancel", cc)
		defer ciw.Close()
		defer cvw.Close()
		defer cew.Close()
		defer ccw.Close()
		logger.Info("workflow cancellation workers registered")
	}

	// CFM lifecycle workers (formation / amendment / movement): the staged
	// object lives in capital-service and validate/execute/cancel write the
	// checker decision back via CapitalCommandService gRPC.
	var capitalClient *capitalclient.Client
	if cfg.CapitalGRPCAddr != "" {
		cc, err := capitalclient.Dial(context.Background(), cfg.CapitalGRPCAddr, cfg.AppName, logger)
		if err != nil {
			logger.Error("capital grpc dial", "err", err)
			os.Exit(1)
		}
		defer cc.Close()
		capitalClient = cc
	}
	for _, flow := range []struct{ kind, prefix string }{
		{worker.CFCKindFormation, "cfc.contract"},
		{worker.CFCKindAmendment, "cfc.amendment"},
		{worker.CFCKindMovement, "cfc.movement"},
	} {
		cfcWorkers := worker.NewCFCWorkers(capitalClient, caseRepo, flow.kind)
		cv, ce, cc := cfcWorkers.Handlers()
		cvw := zeebeSvc.NewJobWorker(flow.prefix+".validate", cv)
		cew := zeebeSvc.NewJobWorker(flow.prefix+".execute", ce)
		ccw := zeebeSvc.NewJobWorker(flow.prefix+".cancel", cc)
		defer cvw.Close()
		defer cew.Close()
		defer ccw.Close()
	}
	logger.Info("workflow capital workers registered")

	var statisticalClient *statisticalclient.Client
	if cfg.StatisticalGRPCAddr != "" {
		sc, err := statisticalclient.Dial(context.Background(), cfg.StatisticalGRPCAddr, cfg.AppName, logger)
		if err != nil {
			logger.Error("statistical grpc dial", "err", err)
			os.Exit(1)
		}
		defer sc.Close()
		statisticalClient = sc
	}

	// RPT submit workers: the submission lifecycle lives in statistical-service;
	// validate/execute/cancel write back via StatisticalCommandService gRPC.
	rptWorkers := worker.NewRPTSubmitWorkers(statisticalClient, caseRepo)
	rv, re, rc := rptWorkers.Handlers()
	rvv := zeebeSvc.NewJobWorker("rpt.submit.validate", rv)
	ree := zeebeSvc.NewJobWorker("rpt.submit.execute", re)
	rcc := zeebeSvc.NewJobWorker("rpt.submit.cancel", rc)
	defer rvv.Close()
	defer ree.Close()
	defer rcc.Close()
	logger.Info("workflow rpt submit workers registered")

	// Handlers
	workflowCmd := service.NewWorkflowCommandService(caseRepo, zeebeSvc)
	serviceSecret, err := identity.SecretFromEnv()
	if err != nil {
		logger.Error("service identity is not configured", "err", err)
		os.Exit(1)
	}
	transportCreds, err := identity.ServerTransportCredentials()
	if err != nil {
		logger.Error("grpc tls is not configured", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerMetadataPropagate(),
			interceptors.UnaryServerServiceAuth(serviceSecret, "workflow-service", map[string]struct{}{
				"crm-service":         {},
				"hrm-service":         {},
				"loan-service":        {},
				"finance-service":     {},
				"statistical-service": {},
				"deposit-service":     {},
			}),
			interceptors.UnaryServerLogging(logger),
		),
	)
	workflowv1.RegisterWorkflowCommandServiceServer(grpcSrv, grpcserver.NewWorkflowServer(workflowCmd))
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)

	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			logger.Error("grpc listen", "err", err)
			os.Exit(1)
		}
		logger.Info("grpc server started", "name", cfg.AppName, "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("grpc server error", "err", err)
			os.Exit(1)
		}
	}()

	wfHandler := handler.NewWorkflowHandler(zeebeSvc, zeebeRest, crmClient, mappingRepo, caseRepo, processDefinitionRepo)
	wfHandler.AssignmentResolver = assignmentResolver
	wfHandler.IncidentIndex = service.NewZeebeIncidentIndex(esURL)
	notiClient, err := notificationclient.New(cfg.NotificationGRPCAddr)
	if err != nil {
		logger.Error("notification grpc client is required; refusing to start", "err", err)
		os.Exit(1)
	}
	defer notiClient.Close()
	wfHandler.SetNotificationClient(notiClient)
	logger.Info("notification gRPC client configured", "addr", cfg.NotificationGRPCAddr)

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      ardahttp.MetricsMiddleware(cfg.AppName, ardahttp.UserTimezoneMiddleware(transport.NewRouter(wfHandler))),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 45 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Service started", "name", cfg.AppName, "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down service", "name", cfg.AppName)
	grpcSrv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "err", err)
	}
}

type iamAdapter struct {
	client *iamclient.Client
}

func (a *iamAdapter) GetUserBatch(ctx context.Context, userIDs []string) (map[string]repository.UserLookupInfo, error) {
	raw, err := a.client.GetUserBatch(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]repository.UserLookupInfo, len(raw))
	for k, v := range raw {
		result[k] = repository.UserLookupInfo{
			ID:        v.ID,
			Name:      v.Name,
			Email:     v.Email,
			AvatarURL: v.AvatarURL,
		}
	}
	return result, nil
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
