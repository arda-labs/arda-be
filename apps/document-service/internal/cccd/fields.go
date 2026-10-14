// Package cccd turns a vision model's reading of a Vietnamese CCCD / Căn
// cước card into normalized, cross-checked fields. It never corrects values:
// it only flags them for the person confirming the result.
package cccd

const DocType = "cccd"

const (
	CardChip2021    = "cccd_chip_2021"
	CardCanCuoc2024 = "can_cuoc_2024"
	CardBarcode     = "cccd_barcode"
	CardUnknown     = "unknown"
	cardNotCCCD     = "not_cccd"
)

// Field flags. Severity (for the UI): invalid_format, mismatch_id_number,
// invalid_date, date_order = error; unknown_province_code,
// expiry_inconsistent, expired = warning; not_found, no_expiry = info.
const (
	FlagInvalidFormat       = "invalid_format"
	FlagMismatchIDNumber    = "mismatch_id_number"
	FlagUnknownProvinceCode = "unknown_province_code"
	FlagInvalidDate         = "invalid_date"
	FlagDateOrder           = "date_order"
	FlagExpiryInconsistent  = "expiry_inconsistent"
	FlagExpired             = "expired"
	FlagNotFound            = "not_found"
	FlagNoExpiry            = "no_expiry"
)

const (
	WarnBackSideMissing    = "back_side_missing"
	WarnCardVersionUnknown = "card_version_unknown"
	WarnImageQualityLow    = "image_quality_low"
)

type Field struct {
	Value *string  `json:"value"`
	Raw   *string  `json:"raw"`
	Flags []string `json:"flags"`
}

func (f *Field) AddFlag(code string) {
	for _, c := range f.Flags {
		if c == code {
			return
		}
	}
	f.Flags = append(f.Flags, code)
}

type Fields struct {
	IDNumber      Field `json:"id_number"`
	FullName      Field `json:"full_name"`
	DateOfBirth   Field `json:"date_of_birth"`
	Sex           Field `json:"sex"`
	Nationality   Field `json:"nationality"`
	PlaceOfOrigin Field `json:"place_of_origin"`
	Residence     Field `json:"residence"`
	IssueDate     Field `json:"issue_date"`
	IssuePlace    Field `json:"issue_place"`
	ExpiryDate    Field `json:"expiry_date"`
}

// Each visits fields in wire order with their wire names.
func (f Fields) Each(fn func(name string, field Field)) {
	fn("id_number", f.IDNumber)
	fn("full_name", f.FullName)
	fn("date_of_birth", f.DateOfBirth)
	fn("sex", f.Sex)
	fn("nationality", f.Nationality)
	fn("place_of_origin", f.PlaceOfOrigin)
	fn("residence", f.Residence)
	fn("issue_date", f.IssueDate)
	fn("issue_place", f.IssuePlace)
	fn("expiry_date", f.ExpiryDate)
}

type Result struct {
	DocType       string   `json:"doc_type"`
	CardVersion   string   `json:"card_version"`
	Fields        Fields   `json:"fields"`
	Warnings      []string `json:"warnings"`
	Model         string   `json:"model"`
	ModelLocation string   `json:"model_location"`
}
