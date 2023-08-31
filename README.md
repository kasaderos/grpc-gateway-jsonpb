# grpc-gateway-jsonpb
your own marshal, when you want to send back grpc->http

## Problem Int64, Uint64 -> string 

[Marshal problem](https://github.com/golang/protobuf/issues/1414)

[Mapping standard](https://protobuf.dev/programming-guides/proto3/#json)

If your front support int64 you can use this encoder to return number (int64)
