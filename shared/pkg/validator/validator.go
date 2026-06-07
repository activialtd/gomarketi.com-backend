// Package validator wraps go-playground/validator/v10 with GoMarket-specific
// field error formatting. It uses json struct tags for field names in errors,
// so API clients always see the same name they sent in the request body.
package validator

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Validator holds a configured validate instance. Create once per service
// at startup with New() and inject it into handlers.
type Validator struct {
	v *validator.Validate
}

// FieldError is a single field validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrors is a slice of FieldError that implements the error interface.
// Handlers can type-assert to this to build structured 422 responses.
type ValidationErrors []FieldError

func (ve ValidationErrors) Error() string {
	parts := make([]string, len(ve))
	for i, e := range ve {
		parts[i] = fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return strings.Join(parts, "; ")
}

// New creates a Validator. The json struct tag is used as the field name in
// errors — if absent, the Go struct field name is used as a fallback.
func New() *Validator {
	v := validator.New()

	// Use JSON field names (what the client sent) rather than Go field names.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})

	return &Validator{v: v}
}

// Validate runs all `validate:"..."` tag rules on the given struct.
// Returns ValidationErrors (implements error) on failure, nil on success.
// Panics if i is not a struct — callers must only pass structs.
func (vl *Validator) Validate(i any) error {
	if err := vl.v.Struct(i); err != nil {
		var ve validator.ValidationErrors
		if !stderrors.As(err, &ve) {
			return err
		}
		out := make(ValidationErrors, len(ve))
		for j, fe := range ve {
			out[j] = FieldError{
				Field:   fe.Field(),
				Message: fieldMessage(fe),
			}
		}
		return out
	}
	return nil
}

// IsValidationError reports whether err is a ValidationErrors value — useful
// for handlers deciding between 400 and 422 responses.
func IsValidationError(err error) bool {
	var ve ValidationErrors
	return stderrors.As(err, &ve)
}

// fieldMessage converts a validator.FieldError to a human-readable string
// that is safe to return directly to API clients.
func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "required_if", "required_with":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "len":
		return fmt.Sprintf("must be exactly %s characters", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", strings.ReplaceAll(fe.Param(), " ", ", "))
	case "uuid", "uuid4":
		return "must be a valid UUID"
	case "url":
		return "must be a valid URL"
	case "numeric":
		return "must contain only digits"
	case "gt":
		return fmt.Sprintf("must be greater than %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", fe.Param())
	case "lt":
		return fmt.Sprintf("must be less than %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", fe.Param())
	default:
		return fmt.Sprintf("failed validation rule: %s", fe.Tag())
	}
}
