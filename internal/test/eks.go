package test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// NewEKSDescribeClusterAPI creates a new TestServer that behaves like the EKS DescribeCluster API.
func NewEKSDescribeClusterAPI(tb testing.TB, resp *eks.DescribeClusterOutput) TestServer {
	wrappedResp := camelCaseDescribeClusterOutput{
		DescribeClusterOutput: resp,
	}
	return NewHTTPSServerForJSON(tb, http.StatusOK, wrappedResp)
}

// camelCaseDescribeClusterOutput wraps DescribeClusterOutput to provide custom JSON marshalling for mock server responses during unit testing.
// Fulfills json Marshaller interface.
type camelCaseDescribeClusterOutput struct {
	*eks.DescribeClusterOutput
}

// MarshalJSON Converts the struct to a map with camelCase keys before marshalling to JSON,
// as required by the AWS API format. Called during json.Marshal().
func (c camelCaseDescribeClusterOutput) MarshalJSON() ([]byte, error) {
	return json.Marshal(toCamelCaseMap(c.DescribeClusterOutput))
}

// toCamelCaseMap recursively converts a struct into a map[string]interface{},
// where all field names are converted from PascalCase to camelCase. Input must be non-circular;
// transforms only field names and not values.
func toCamelCaseMap(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr && !val.IsNil() {
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		return convertStructToCamelCaseMap(val)
	default:
		// No field names to convert, return unmodified
		if val.CanInterface() {
			return val.Interface()
		}
		return nil
	}
}

func convertStructToCamelCaseMap(val reflect.Value) map[string]interface{} {
	result := make(map[string]interface{})
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if !field.CanInterface() {
			continue
		}
		fieldName := typ.Field(i).Name
		fieldName = strings.ToLower(fieldName[:1]) + fieldName[1:]
		result[fieldName] = toCamelCaseMap(field.Interface())
	}
	return result
}
