package cloudmap

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"

	"github.com/micro/go-micro/registry"
)

func encode(buf []byte) string {
	var b bytes.Buffer
	defer b.Reset()

	w := zlib.NewWriter(&b)
	if _, err := w.Write(buf); err != nil {
		return ""
	}
	w.Close()

	return hex.EncodeToString(b.Bytes())
}

func decode(d string) []byte {
	hr, err := hex.DecodeString(d)
	if err != nil {
		return nil
	}

	br := bytes.NewReader(hr)
	zr, err := zlib.NewReader(br)
	if err != nil {
		return nil
	}

	rbuf, err := ioutil.ReadAll(zr)
	if err != nil {
		return nil
	}

	return rbuf
}

func encodeEndpoints(endpoints []*registry.Endpoint) string {
	// Should probably move this multiple tag entries instead of one massive entry
	var encoded string
	b, err := json.Marshal(endpoints)
	if err == nil {
		encoded = encode(b)
	}

	return encoded
}

func decodeEndpoints(encoded string) []*registry.Endpoint {
	var rsp []*registry.Endpoint
	buf := decode(encoded)
	json.Unmarshal(buf, &rsp)
	return rsp
}

func encodeMetadata(md map[string]string) string {
	// Same here, need to turn this into multiple tags
	var encoded string
	b, err := json.Marshal(md)
	if err == nil {
		encoded = encode(b)
	}
	return encoded
}

func decodeMetadata(encoded string) map[string]string {
	md := make(map[string]string)
	buf := decode(encoded)
	json.Unmarshal(buf, &md)
	return md
}

func encodeVersion(v string) string {
	return encode([]byte(v))
}

func decodeVersion(encoded string) string {
	return string(decode(encoded))
}

