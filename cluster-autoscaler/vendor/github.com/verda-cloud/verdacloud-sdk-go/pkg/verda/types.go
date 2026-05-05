// types.go contains generic utility types that are not tied to any specific
// service domain. Domain-specific types (structs, constants, Validate methods)
// live in the corresponding *_types.go file for each service.
//
// Only add types here if they are used across multiple service domains and
// have no natural home in a single service file.

package verda

import (
	"encoding/json"
	"strconv"
)

// FlexibleFloat is a custom type that can unmarshal both string and float64 values
type FlexibleFloat float64

// UnmarshalJSON implements json.Unmarshaler to handle both string and float64 inputs
func (f *FlexibleFloat) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as float64 first
	var floatVal float64
	if err := json.Unmarshal(data, &floatVal); err == nil {
		*f = FlexibleFloat(floatVal)
		return nil
	}

	// Try to unmarshal as string
	var strVal string
	if err := json.Unmarshal(data, &strVal); err != nil {
		return err
	}

	floatVal, err := strconv.ParseFloat(strVal, 64)
	if err != nil {
		return err
	}

	*f = FlexibleFloat(floatVal)
	return nil
}

// MarshalJSON implements json.Marshaler to always marshal as float64
func (f FlexibleFloat) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(f))
}

// Float64 returns the float64 value
func (f FlexibleFloat) Float64() float64 {
	return float64(f)
}
