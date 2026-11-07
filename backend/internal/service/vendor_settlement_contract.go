package service

import (
	"sort"
	"strconv"
	"time"
)

// Historical rates are explicit platform administrator attestations. Supplier
// payers use only their direct child's current contract. Only the current
// single-version suggestion can inherit current vendor defaults.
type VendorSettlementContractPeriod struct {
	StartDate          string            `json:"start_date"`
	EndDate            string            `json:"end_date"`
	ContractVersion    int64             `json:"contract_version"`
	DefaultCoefficient string            `json:"default_coefficient"`
	GroupCoefficients  map[string]string `json:"group_coefficients"`
	start              time.Time
	end                time.Time
}

func vendorSettlementContracts(in VendorSettlementInput, f VendorUsageFilter) ([]VendorSettlementContractPeriod, error) {
	if in.PayerVendorID != nil && len(in.ContractPeriods) != 0 {
		return nil, ErrVendorUsageInput
	}
	periods := append([]VendorSettlementContractPeriod(nil), in.ContractPeriods...)
	if len(periods) == 0 {
		periods = []VendorSettlementContractPeriod{{StartDate: in.StartDate, EndDate: in.EndDate, ContractVersion: in.ContractVersion, DefaultCoefficient: in.DefaultCoefficient, GroupCoefficients: in.GroupCoefficients}}
	}
	if len(periods) > 31 {
		return nil, ErrVendorUsageInput
	}
	loc, e := time.LoadLocation(f.Timezone)
	if e != nil {
		return nil, ErrVendorUsageInput
	}
	sort.Slice(periods, func(i, j int) bool { return periods[i].StartDate < periods[j].StartDate })
	next := f.start
	for i := range periods {
		p := &periods[i]
		start, e1 := time.ParseInLocation(time.DateOnly, p.StartDate, loc)
		end, e2 := time.ParseInLocation(time.DateOnly, p.EndDate, loc)
		if e1 != nil || e2 != nil || end.Before(start) || !start.Equal(next) || p.ContractVersion <= 0 || p.ContractVersion > in.ContractVersion {
			return nil, ErrVendorUsageInput
		}
		p.start = start
		p.end = end.AddDate(0, 0, 1)
		next = p.end
		d, e := vendorDecimal(p.DefaultCoefficient, 10, false)
		if e != nil {
			return nil, e
		}
		p.DefaultCoefficient = d.String()
		rates := make(map[string]string, len(p.GroupCoefficients))
		if len(p.GroupCoefficients) > 1000 {
			return nil, ErrVendorUsageInput
		}
		for k, v := range p.GroupCoefficients {
			id, e := strconv.ParseInt(k, 10, 64)
			if e != nil || id <= 0 || k != strconv.FormatInt(id, 10) {
				return nil, ErrVendorUsageInput
			}
			rate, e := vendorDecimal(v, 10, false)
			if e != nil {
				return nil, e
			}
			rates[k] = rate.String()
		}
		p.GroupCoefficients = rates
	}
	if !next.Equal(f.end) {
		return nil, ErrVendorUsageInput
	}
	return periods, nil
}
func vendorSettlementEntryRate(periods []VendorSettlementContractPeriod, e VendorUsageEntry) (int64, string, error) {
	for _, p := range periods {
		if !e.CreatedAt.Before(p.start) && e.CreatedAt.Before(p.end) {
			rate := p.DefaultCoefficient
			if value, ok := p.GroupCoefficients[vendorOptionalID(e.GroupID)]; ok {
				rate = value
			}
			return p.ContractVersion, rate, nil
		}
	}
	return 0, "", ErrVendorSettlementConflict
}
func vendorSettlementLineKey(group *int64, version int64) string {
	return vendorOptionalID(group) + ":" + strconv.FormatInt(version, 10)
}
