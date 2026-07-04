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

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/workflow-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/workflow-service/internal/config"
	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
	"github.com/arda-labs/arda/apps/workflow-service/internal/migration"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	grpcserver "github.com/arda-labs/arda/apps/workflow-service/internal/transport/grpc"
	transport "github.com/arda-labs/arda/apps/workflow-service/internal/transport/http"
	"github.com/arda-labs/arda/apps/workflow-service/internal/worker"
	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
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
	db, err := sql.Open("postgres", cfg.DatabaseDSN)
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

	crmWorkers := worker.NewCRMWorkers(crmClient)
	crmMarkSubmittedWorker := zeebeSvc.NewJobWorker("crm.mark_customer_submitted", crmWorkers.MarkSubmittedHandler)
	defer crmMarkSubmittedWorker.Close()
	crmCheckDuplicateWorker := zeebeSvc.NewJobWorker("crm.check_customer_duplicate", crmWorkers.CheckDuplicateHandler)
	defer crmCheckDuplicateWorker.Close()
	crmRequestChangesWorker := zeebeSvc.NewJobWorker("crm.request_customer_changes", crmWorkers.RequestChangesHandler)
	defer crmRequestChangesWorker.Close()
	crmRejectWorker := zeebeSvc.NewJobWorker("crm.reject_customer", crmWorkers.RejectCustomerHandler)
	defer crmRejectWorker.Close()
	crmCreateWorker := zeebeSvc.NewJobWorker("crm.create_customer", crmWorkers.CreateCustomerHandler)
	defer crmCreateWorker.Close()
	crmUpdateWorker := zeebeSvc.NewJobWorker("crm.update_customer", crmWorkers.UpdateCustomerHandler)
	defer crmUpdateWorker.Close()
	crmApproveWorker := zeebeSvc.NewJobWorker("crm.approve_customer", crmWorkers.ApproveCustomerHandler)
	defer crmApproveWorker.Close()
	logger.Info("workflow CRM job workers registered")

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

	// Handlers
	workflowCmd := service.NewWorkflowCommandService(caseRepo, zeebeSvc)
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(interceptors.UnaryServerLogging(logger)))
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

	wfHandler := handler.NewWorkflowHandler(zeebeSvc, mappingRepo, caseRepo, processDefinitionRepo)

	// Router and HTTP Server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      transport.NewRouter(wfHandler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
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
