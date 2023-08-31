// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonpb

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mechta-market/nsi/internal/domain/lib/jsonpb/errors"
	"github.com/mechta-market/nsi/internal/domain/lib/jsonpb/genid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type marshalFunc func(encoder, protoreflect.Message) error

// wellKnownTypeMarshaler returns a marshal function if the message type
// has specialized serialization behavior. It returns nil otherwise.
func wellKnownTypeMarshaler(name protoreflect.FullName) marshalFunc {
	if name.Parent() == genid.GoogleProtobuf_package {
		switch name.Name() {
		case genid.Any_message_name:
			return encoder.marshalAny
		case genid.Timestamp_message_name:
			return encoder.marshalTimestamp
		case genid.Duration_message_name:
			return encoder.marshalDuration
		case genid.BoolValue_message_name,
			genid.Int32Value_message_name,
			genid.Int64Value_message_name,
			genid.UInt32Value_message_name,
			genid.UInt64Value_message_name,
			genid.FloatValue_message_name,
			genid.DoubleValue_message_name,
			genid.StringValue_message_name,
			genid.BytesValue_message_name:
			return encoder.marshalWrapperType
		case genid.Struct_message_name:
			return encoder.marshalStruct
		case genid.ListValue_message_name:
			return encoder.marshalListValue
		case genid.Value_message_name:
			return encoder.marshalKnownValue
		case genid.FieldMask_message_name:
			return encoder.marshalFieldMask
		case genid.Empty_message_name:
			return encoder.marshalEmpty
		}
	}
	return nil
}

// The JSON representation of an Any message uses the regular representation of
// the deserialized, embedded message, with an additional field `@type` which
// contains the type URL. If the embedded message type is well-known and has a
// custom JSON representation, that representation will be embedded adding a
// field `value` which holds the custom JSON in addition to the `@type` field.

func (e encoder) marshalAny(m protoreflect.Message) error {
	fds := m.Descriptor().Fields()
	fdType := fds.ByNumber(genid.Any_TypeUrl_field_number)
	fdValue := fds.ByNumber(genid.Any_Value_field_number)

	if !m.Has(fdType) {
		if !m.Has(fdValue) {
			// If message is empty, marshal out empty JSON object.
			e.StartObject()
			e.EndObject()
			return nil
		} else {
			// Return error if type_url field is not set, but value is set.
			return errors.New("%s: %v is not set", genid.Any_message_fullname, genid.Any_TypeUrl_field_name)
		}
	}

	typeVal := m.Get(fdType)
	valueVal := m.Get(fdValue)

	// Resolve the type in order to unmarshal value field.
	typeURL := typeVal.String()
	emt, err := e.opts.Resolver.FindMessageByURL(typeURL)
	if err != nil {
		return errors.New("%s: unable to resolve %q: %v", genid.Any_message_fullname, typeURL, err)
	}

	em := emt.New()
	err = proto.UnmarshalOptions{
		AllowPartial: true, // never check required fields inside an Any
		Resolver:     e.opts.Resolver,
	}.Unmarshal(valueVal.Bytes(), em.Interface())
	if err != nil {
		return errors.New("%s: unable to unmarshal %q: %v", genid.Any_message_fullname, typeURL, err)
	}

	// If type of value has custom JSON encoding, marshal out a field "value"
	// with corresponding custom JSON encoding of the embedded message as a
	// field.
	if marshal := wellKnownTypeMarshaler(emt.Descriptor().FullName()); marshal != nil {
		e.StartObject()
		defer e.EndObject()

		// Marshal out @type field.
		e.WriteName("@type")
		if err := e.WriteString(typeURL); err != nil {
			return err
		}

		e.WriteName("value")
		return marshal(e, em)
	}

	// Else, marshal out the embedded message's fields in this Any object.
	if err := e.marshalMessage(em, typeURL); err != nil {
		return err
	}

	return nil
}

// Wrapper types are encoded as JSON primitives like string, number or boolean.

func (e encoder) marshalWrapperType(m protoreflect.Message) error {
	fd := m.Descriptor().Fields().ByNumber(genid.WrapperValue_Value_field_number)
	val := m.Get(fd)
	return e.marshalSingular(val, fd)
}

// The JSON representation for Empty is an empty JSON object.

func (e encoder) marshalEmpty(protoreflect.Message) error {
	e.StartObject()
	e.EndObject()
	return nil
}

// The JSON representation for Struct is a JSON object that contains the encoded
// Struct.fields map and follows the serialization rules for a map.

func (e encoder) marshalStruct(m protoreflect.Message) error {
	fd := m.Descriptor().Fields().ByNumber(genid.Struct_Fields_field_number)
	return e.marshalMap(m.Get(fd).Map(), fd)
}

// The JSON representation for ListValue is JSON array that contains the encoded
// ListValue.values repeated field and follows the serialization rules for a
// repeated field.

func (e encoder) marshalListValue(m protoreflect.Message) error {
	fd := m.Descriptor().Fields().ByNumber(genid.ListValue_Values_field_number)
	return e.marshalList(m.Get(fd).List(), fd)
}

// The JSON representation for a Value is dependent on the oneof field that is
// set. Each of the field in the oneof has its own custom serialization rule. A
// Value message needs to be a oneof field set, else it is an error.

func (e encoder) marshalKnownValue(m protoreflect.Message) error {
	od := m.Descriptor().Oneofs().ByName(genid.Value_Kind_oneof_name)
	fd := m.WhichOneof(od)
	if fd == nil {
		return errors.New("%s: none of the oneof fields is set", genid.Value_message_fullname)
	}
	if fd.Number() == genid.Value_NumberValue_field_number {
		if v := m.Get(fd).Float(); math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("%s: invalid %v value", genid.Value_NumberValue_field_fullname, v)
		}
	}
	return e.marshalSingular(m.Get(fd), fd)
}

// The JSON representation for a Duration is a JSON string that ends in the
// suffix "s" (indicating seconds) and is preceded by the number of seconds,
// with nanoseconds expressed as fractional seconds.
//
// Durations less than one second are represented with a 0 seconds field and a
// positive or negative nanos field. For durations of one second or more, a
// non-zero value for the nanos field must be of the same sign as the seconds
// field.
//
// Duration.seconds must be from -315,576,000,000 to +315,576,000,000 inclusive.
// Duration.nanos must be from -999,999,999 to +999,999,999 inclusive.

const (
	secondsInNanos       = 999999999
	maxSecondsInDuration = 315576000000
)

func (e encoder) marshalDuration(m protoreflect.Message) error {
	fds := m.Descriptor().Fields()
	fdSeconds := fds.ByNumber(genid.Duration_Seconds_field_number)
	fdNanos := fds.ByNumber(genid.Duration_Nanos_field_number)

	secsVal := m.Get(fdSeconds)
	nanosVal := m.Get(fdNanos)
	secs := secsVal.Int()
	nanos := nanosVal.Int()
	if secs < -maxSecondsInDuration || secs > maxSecondsInDuration {
		return errors.New("%s: seconds out of range %v", genid.Duration_message_fullname, secs)
	}
	if nanos < -secondsInNanos || nanos > secondsInNanos {
		return errors.New("%s: nanos out of range %v", genid.Duration_message_fullname, nanos)
	}
	if (secs > 0 && nanos < 0) || (secs < 0 && nanos > 0) {
		return errors.New("%s: signs of seconds and nanos do not match", genid.Duration_message_fullname)
	}
	// Generated output always contains 0, 3, 6, or 9 fractional digits,
	// depending on required precision, followed by the suffix "s".
	var sign string
	if secs < 0 || nanos < 0 {
		sign, secs, nanos = "-", -1*secs, -1*nanos
	}
	x := fmt.Sprintf("%s%d.%09d", sign, secs, nanos)
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, ".000")
	e.WriteString(x + "s")
	return nil
}

// The JSON representation for a Timestamp is a JSON string in the RFC 3339
// format, i.e. "{year}-{month}-{day}T{hour}:{min}:{sec}[.{frac_sec}]Z" where
// {year} is always expressed using four digits while {month}, {day}, {hour},
// {min}, and {sec} are zero-padded to two digits each. The fractional seconds,
// which can go up to 9 digits, up to 1 nanosecond resolution, is optional. The
// "Z" suffix indicates the timezone ("UTC"); the timezone is required. Encoding
// should always use UTC (as indicated by "Z") and a decoder should be able to
// accept both UTC and other timezones (as indicated by an offset).
//
// Timestamp.seconds must be from 0001-01-01T00:00:00Z to 9999-12-31T23:59:59Z
// inclusive.
// Timestamp.nanos must be from 0 to 999,999,999 inclusive.

const (
	maxTimestampSeconds = 253402300799
	minTimestampSeconds = -62135596800
)

func (e encoder) marshalTimestamp(m protoreflect.Message) error {
	fds := m.Descriptor().Fields()
	fdSeconds := fds.ByNumber(genid.Timestamp_Seconds_field_number)
	fdNanos := fds.ByNumber(genid.Timestamp_Nanos_field_number)

	secsVal := m.Get(fdSeconds)
	nanosVal := m.Get(fdNanos)
	secs := secsVal.Int()
	nanos := nanosVal.Int()
	if secs < minTimestampSeconds || secs > maxTimestampSeconds {
		return errors.New("%s: seconds out of range %v", genid.Timestamp_message_fullname, secs)
	}
	if nanos < 0 || nanos > secondsInNanos {
		return errors.New("%s: nanos out of range %v", genid.Timestamp_message_fullname, nanos)
	}
	// Uses RFC 3339, where generated output will be Z-normalized and uses 0, 3,
	// 6 or 9 fractional digits.
	t := time.Unix(secs, nanos).UTC()
	x := t.Format("2006-01-02T15:04:05.000000000")
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, ".000")
	e.WriteString(x + "Z")
	return nil
}

// The JSON representation for a FieldMask is a JSON string where paths are
// separated by a comma. Fields name in each path are converted to/from
// lower-camel naming conventions. Encoding should fail if the path name would
// end up differently after a round-trip.

func (e encoder) marshalFieldMask(m protoreflect.Message) error {
	fd := m.Descriptor().Fields().ByNumber(genid.FieldMask_Paths_field_number)
	list := m.Get(fd).List()
	paths := make([]string, 0, list.Len())

	for i := 0; i < list.Len(); i++ {
		s := list.Get(i).String()
		if !protoreflect.FullName(s).IsValid() {
			return errors.New("%s contains invalid path: %q", genid.FieldMask_Paths_field_fullname, s)
		}
		// Return error if conversion to camelCase is not reversible.
		cc := JSONCamelCase(s)
		if s != JSONSnakeCase(cc) {
			return errors.New("%s contains irreversible value %q", genid.FieldMask_Paths_field_fullname, s)
		}
		paths = append(paths, cc)
	}

	e.WriteString(strings.Join(paths, ","))
	return nil
}
