package v1

import (
	"net/url"

	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/utils/object/filter"
	"golang.org/x/net/context"
)

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

type commonRequestBody struct {
	State string `json:"state,omitempty"`
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
