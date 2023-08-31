package jsonpb

import (
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TestStruct struct {
	Id        string
	ManagerId int64
	CreatedAt *timestamppb.Timestamp
}

func TestJSONPbMarshal(t *testing.T) {
	// our marshaller
	pb := &JSONPb{}
	marshaler := runtime.Marshaler(pb)

	createdAt := time.Date(2023, 8, 29, 0, 0, 0, 0, time.UTC)

	st := &TestStruct{
		Id:        "id",
		ManagerId: 1 << 54,
		CreatedAt: timestamppb.New(createdAt),
	}
	actual, err := marshaler.Marshal(st)
	require.Nil(t, err)

	expected := []byte(`{"id":"id","createdAt":"2023-08-29T00:00:00Z","managerId":18014398509481984}`)
	require.Equal(t, expected, actual)
}
