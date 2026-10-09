package paramspec

import ardaParams "github.com/arda-labs/arda/libs/go/arda-params"

const GeneralProvisionRate = "LNM_GENERAL_PROVISION_RATE"

func LoanModule() ardaParams.ModuleSpec {
	return ardaParams.ModuleSpec{Name: "loan", Params: []ardaParams.ParamSpec{{
		Code: GeneralProvisionRate, Type: ardaParams.TypeDecimal, Unit: "percent", Scope: ardaParams.ScopeOrg, Required: true,
	}}}
}
