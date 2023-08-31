package jsonpb

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// JSONPb is a Marshaler which marshals/unmarshals into/from JSON
// with the "google.golang.org/protobuf/encoding/protojson" marshaler.
// It supports the full functionality of protobuf unlike JSONBuiltin.
//
// The NewDecoder method returns a DecoderWrapper, so the underlying
// *json.Decoder methods can be used.
type JSONPb struct {
	MarshalOptions
	protojson.UnmarshalOptions
}

// ContentType always returns "application/json".
func (*JSONPb) ContentType(_ interface{}) string {
	return "application/json"
}

// Marshal marshals "v" into JSON.
func (j *JSONPb) Marshal(v interface{}) ([]byte, error) {
	if _, ok := v.(proto.Message); !ok {
		return json.Marshal(v)
	}

	var buf bytes.Buffer
	if err := j.marshalTo(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (j *JSONPb) marshalTo(w io.Writer, v interface{}) error {
	p, ok := v.(proto.Message)
	if !ok {
		buf, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(buf)
		return err
	}
	b, err := j.MarshalOptions.Marshal(p)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	return err
}

// Unmarshal unmarshals JSON "data" into "v"
func (j *JSONPb) Unmarshal(data []byte, v interface{}) error {
	return unmarshalJSONPb(data, j.UnmarshalOptions, v)
}

// NewDecoder returns a Decoder which reads JSON stream from "r".
func (j *JSONPb) NewDecoder(r io.Reader) runtime.Decoder {
	d := json.NewDecoder(r)
	return DecoderWrapper{
		Decoder:          d,
		UnmarshalOptions: j.UnmarshalOptions,
	}
}

// DecoderWrapper is a wrapper around a *json.Decoder that adds
// support for protos to the Decode method.
type DecoderWrapper struct {
	*json.Decoder
	protojson.UnmarshalOptions
}

// NewEncoder returns an Encoder which writes JSON stream into "w".
func (j *JSONPb) NewEncoder(w io.Writer) runtime.Encoder {
	return EncoderFunc(func(v interface{}) error {
		if err := j.marshalTo(w, v); err != nil {
			return err
		}
		// mimic json.Encoder by adding a newline (makes output
		// easier to read when it contains multiple encoded items)
		_, err := w.Write(j.Delimiter())
		return err
	})
}

func unmarshalJSONPb(data []byte, unmarshaler protojson.UnmarshalOptions, v interface{}) error {
	p, ok := v.(proto.Message)
	if !ok {
		return json.Unmarshal(data, v)
	}

	d := json.NewDecoder(bytes.NewReader(data))
	// Decode into bytes for marshalling
	var b json.RawMessage
	if err := d.Decode(&b); err != nil {
		return err
	}

	return unmarshaler.Unmarshal([]byte(b), p)
}

func (j *JSONPb) Delimiter() []byte {
	return []byte("\n")
}
