package mockapi

import (
	_ "github.com/golang/mock/gomock"
)

//go:generate go run github.com/golang/mock/mockgen@v1.6.0 -package mockapi -destination xxgenerated_api.go go.anx.io/go-anxcloud/pkg/api/types API
