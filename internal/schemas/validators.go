package schemas

import (
	"reflect"
	"regexp"
	"strconv"

	"github.com/go-playground/validator/v10"
)

var quantityPattern = regexp.MustCompile(`^\d+(\.\d{1,4})?$`)

// validateQuantity rejects non-positive, over-precision, or out-of-range quantities.
func validateQuantity(fl validator.FieldLevel) bool {
	s, ok := fl.Field().String(), fl.Field().Kind() == reflect.String
	if !ok || !quantityPattern.MatchString(s) {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	return f > 0 && f < 1000000
}

// NewValidator returns *validator.Validate with custom rules registered
func NewValidator() *validator.Validate {
	v := validator.New()
	_ = v.RegisterValidation("quantity", validateQuantity)
	return v
}
