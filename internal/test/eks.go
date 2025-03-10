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
	wrappedResp := CustomDescribeClusterOutput{
		DescribeClusterOutput: resp,
	}
	return NewHTTPSServerForJSON(tb, http.StatusOK, wrappedResp)
}

// CustomDescribeClusterOutput wraps DescribeClusterOutput to provide custom JSON marshalling for mock server responses during unit testing.
// Fulfills json Marshaller interface.
type CustomDescribeClusterOutput struct {
	*eks.DescribeClusterOutput
}

// MarshalJSON Converts the struct to a map with camelCase keys before marshalling to JSON,
// as required by the AWS API format. Called during json.Marshal().
func (c CustomDescribeClusterOutput) MarshalJSON() ([]byte, error) {
	return json.Marshal(convertToLowerCamelCase(c.DescribeClusterOutput))
}

// convertToLowerCamelCase recursively converts a struct into a map[string]interface{},
// where all field names are converted from PascalCase to camelCase. It handles nested
// structs, slices, arrays, and maps, preserving the original values while only
// transforming the field names.
func convertToLowerCamelCase(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr && !val.IsNil() {
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		return handleStruct(val)
	case reflect.Slice, reflect.Array:
		return handleSliceOrArray(val)
	case reflect.Map:
		return handleMap(val)
	default:
		if val.CanInterface() {
			return val.Interface()
		}
		return nil
	}
}

func handleStruct(val reflect.Value) map[string]interface{} {
	result := make(map[string]interface{})
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if !field.CanInterface() {
			continue
		}
		fieldName := typ.Field(i).Name
		fieldName = strings.ToLower(fieldName[:1]) + fieldName[1:]
		result[fieldName] = convertToLowerCamelCase(field.Interface())
	}
	return result
}

func handleSliceOrArray(val reflect.Value) []interface{} {
	result := make([]interface{}, 0, val.Len())
	for i := 0; i < val.Len(); i++ {
		elem := val.Index(i)
		if elem.CanInterface() {
			result = append(result, convertToLowerCamelCase(elem.Interface()))
		}
	}
	return result
}

func handleMap(val reflect.Value) map[string]interface{} {
	result := make(map[string]interface{})
	for _, key := range val.MapKeys() {
		mapVal := val.MapIndex(key)
		if mapVal.CanInterface() {
			result[key.String()] = convertToLowerCamelCase(mapVal.Interface())
		}
	}
	return result
}
