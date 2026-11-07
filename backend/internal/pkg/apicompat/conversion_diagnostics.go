package apicompat

// ConversionDiagnostic describes structure only. Callers use constant codes and
// paths, never content, property names, tool arguments or reasoning signatures.
type ConversionDiagnostic struct {
	Code string `json:"code"`
	Path string `json:"path"`
}
type ConversionDiagnostics []ConversionDiagnostic

func (d *ConversionDiagnostics) Add(code, path string) {
	if d == nil || len(*d) >= 32 || len(code) > 64 || len(path) > 128 {
		return
	}
	item := ConversionDiagnostic{Code: code, Path: path}
	for _, prior := range *d {
		if prior == item {
			return
		}
	}
	*d = append(*d, item)
}
