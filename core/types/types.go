package types

import "fmt"

// Failure is a contract error returned to callers. It is not an internal panic.
type Failure struct {
	Code    string
	Message string
	Status  int
}

func (f *Failure) Error() string { return f.Code + ": " + f.Message }

func Fail(code, message string, status int) *Failure {
	return &Failure{Code: code, Message: message, Status: status}
}

func As(err error) *Failure {
	var f *Failure
	if err != nil && AsError(err, &f) {
		return f
	}
	return nil
}

func AsError(err error, target **Failure) bool {
	for err != nil {
		if f, ok := err.(*Failure); ok {
			*target = f
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

type VectorProfile struct {
	ID        string
	ModelID   string
	Dimension int
	Revision  string
}

type Metric struct {
	Value               int    `json:"value"`
	Unit                string `json:"unit"`
	Accuracy            string `json:"accuracy"`
	CalculationComplete bool   `json:"calculation_complete"`
	Scope               string `json:"scope"`
}

type ProjectionCoverage struct {
	Projection  string   `json:"projection"`
	State       string   `json:"state"`
	Unit        string   `json:"unit"`
	Total       *int     `json:"total"`
	Ready       *int     `json:"ready"`
	Failed      *int     `json:"failed"`
	Pending     *int     `json:"pending"`
	Excluded    *int     `json:"excluded"`
	ObservedAt  string   `json:"observed_at"`
	Scope       string   `json:"scope"`
	ReasonCodes []string `json:"reason_codes"`
}

type Coverage struct {
	ExecutionPartial bool                 `json:"execution_partial"`
	DisplayTruncated bool                 `json:"display_truncated"`
	Approximate      bool                 `json:"approximate"`
	Consistency      string               `json:"consistency"`
	ScopeBasis       string               `json:"scope_basis"`
	Notes            []string             `json:"notes,omitempty"`
	IndexCoverage    []ProjectionCoverage `json:"index_coverage"`
}

type Handle struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Basis            string `json:"basis"`
	Complete         bool   `json:"complete"`
	Count            int    `json:"count"`
	SourceRestricted bool   `json:"source_restricted"`
	ExpiresAt        string `json:"expires_at"`
}

type Item struct {
	Kind      string `json:"kind"`
	KB        string `json:"kb"`
	Path      string `json:"path"`
	Edition   string `json:"edition"`
	ChunkID   string `json:"chunk_id,omitempty"`
	ChunkKey  string `json:"chunk_key,omitempty"`
	FileHash  string `json:"file_hash,omitempty"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Ref       string `json:"ref"`
}

type Response struct {
	Action   string   `json:"action"`
	Items    []Item   `json:"items"`
	Total    Metric   `json:"total"`
	Coverage Coverage `json:"coverage"`
	Handle   *Handle  `json:"handle,omitempty"`
	ResultID string   `json:"result_id,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

func Exact(value int, unit, scope string) Metric {
	return Metric{Value: value, Unit: unit, Accuracy: "exact", CalculationComplete: true, Scope: scope}
}

func Check(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Failure); ok {
		return err
	}
	return fmt.Errorf("%w", err)
}
