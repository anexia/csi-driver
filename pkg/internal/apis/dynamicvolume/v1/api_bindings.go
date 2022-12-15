package controller

import (
	"net/url"
	"strings"

	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	"go.anx.io/go-anxcloud/pkg/utils/object/filter"
	"golang.org/x/net/context"
)

var _ types.Object = &Volume{}
var _ types.Object = &StorageServerInterface{}

//var _ types.Object = &Prefix{}

func endpointURL(ctx context.Context, o types.Object, apiPath string) (*url.URL, error) {
	op, err := types.OperationFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if op == types.OperationUpdate {
		return nil, api.ErrOperationNotSupported
	}

	// we can ignore the error since the URL is hard-coded known as valid
	u, _ := url.Parse(apiPath)

	if op == types.OperationList {
		helper, err := filter.NewHelper(o)
		if err != nil {
			return nil, err
		}

		filters := helper.BuildQuery().Encode()

		if filters != "" {
			query := u.Query()
			query.Set("filters", filters)
			u.RawQuery = query.Encode()
		}
	}

	return u, nil
}

type Volume struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Name       string `json:"name,omitempty"`

	StorageServerInterfaces *[]StorageServerInterface `json:"storage_server_interfaces,omitempty"`
	Prefixes                *[]Prefix                 `json:"prefixes,omitempty"`

	ADSClass string `json:"ads_class,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (v *Volume) FilterAPIRequestBody(ctx context.Context) (interface{}, error) {
	return requestBody(ctx, func() interface{} {
		return &struct {
			commonRequestBody
			Volume
			StorageServerInterfaces *string `json:"storage_server_interfaces,omitempty"`
			Prefixes                *string `json:"prefixes,omitempty"`
		}{
			Volume: *v,

			StorageServerInterfaces: joinPointerString(mapPointerSlice(func(s StorageServerInterface) string {
				return s.Identifier
			}, v.StorageServerInterfaces)),
			Prefixes: joinPointerString(mapPointerSlice(func(p Prefix) string {
				return p.Identifier
			}, v.Prefixes)),
		}
	})
}

type commonRequestBody struct {
	State string `json:"state,omitempty"`
}

func mapPointerSlice[T, U any](f func(T) U, in *[]T) *[]U {
	if in == nil {
		return nil
	}
	out := make([]U, 0, len(*in))
	for _, v := range *in {
		out = append(out, f(v))
	}
	return &out
}

func joinPointerString(in *[]string) *string {
	if in == nil {
		return nil
	}
	out := strings.Join(*in, ",")
	return &out
}

func requestBody(ctx context.Context, br func() interface{}) (interface{}, error) {
	op, err := types.OperationFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if op == types.OperationCreate || op == types.OperationUpdate {
		response := br()

		return response, nil
	}

	return nil, nil
}

func (v *Volume) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, v, "/api/dynamic_volume/v1/volumes.json")
}

func (v *Volume) GetIdentifier(ctx context.Context) (string, error) {
	return v.Identifier, nil
}

type StorageServerInterface struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Name       string `json:"name,omitempty"`

	IPAddress IPAddress       `json:"ip_address"`
	Location  corev1.Location `json:"location,omitempty"`
}

func (s *StorageServerInterface) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, s, "/api/dynamic_volume/v1/storage_server_interfaces.json")
}

func (s *StorageServerInterface) GetIdentifier(ctx context.Context) (string, error) {
	return s.Identifier, nil
}

type Prefix struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Prefix     string `json:"prefix,omitempty"`
}

type IPAddress struct {
	Identifier string `json:"identifier,omitempty"`
	Name       string `json:"name,omitempty"`
}
