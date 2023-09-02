// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// Request contains the data to send to the backend
type Request struct {
	Method  string
	URL     *url.URL
	Query   url.Values
	Path    string
	Body    io.ReadCloser
	Params  map[string]string
	Headers map[string][]string

	Data          map[string][]map[string]interface{} // 最小存储单元:<dataType,dataList>
	Private       map[string]interface{}              // 存储一些私有数据
	Reserved      map[string]interface{}              // Pipeline转换专用
	RemoteAddr    string                              // 远程地址
	ContentLength int64                               // 请求长度
}

// ParseID 以/为分隔符解析URL最末尾的id.
func (r *Request) ParseID() string {
	URL := r.URL.String()
	index := strings.LastIndex(URL, "/")
	return URL[index+1:]
}

// Snapshot 获取数据个数, s用于记录日志.
func (r *Request) Snapshot() string {
	var s = fmt.Sprintf("[%s %s %s] ", r.SourceIP(), r.Method, r.Path)
	if len(r.Data) != 0 {
		for dataType, arr := range r.Data {
			s += fmt.Sprintf("'%s' %d; ", dataType, len(arr))
		}
	} else {
		s += "no data; "
	}
	return s
}

// SourceIP 获取用户IP的标准姿势: https://zhuanlan.zhihu.com/p/21354318
func (r *Request) SourceIP() string {
	if r == nil {
		return "127.0.0.2"
	}
	if h := r.HeaderGet("X-Real-IP"); h != "" {
		return h
	}
	// X-Forwarded-For: RFC 7239  http://tools.ietf.org/html/rfc7239
	if h := r.HeaderGet("X-Forwarded-For"); h != "" {
		n := strings.Index(h, ",")
		if n == -1 {
			return h
		}
		return h[:n]
	}
	s := strings.Index(r.RemoteAddr, ":")
	if s == -1 {
		return r.RemoteAddr
	}
	return r.RemoteAddr[:s]
}

// GeneratePath takes a pattern and updates the path of the request
func (r *Request) GeneratePath(URLPattern string) {
	if len(r.Params) == 0 {
		r.Path = URLPattern
		return
	}
	buff := []byte(URLPattern)
	for k, v := range r.Params {
		key := []byte{}
		key = append(key, "{{."...)
		key = append(key, k...)
		key = append(key, "}}"...)
		buff = bytes.ReplaceAll(buff, key, []byte(v))
	}
	r.Path = string(buff)
}

// Clone clones itself into a new request. The returned cloned request is not
// thread-safe, so changes on request.Params and request.Headers could generate
// race-conditions depending on the part of the pipe they are being executed.
// For thread-safe request headers and/or params manipulation, use the proxy.CloneRequest
// function.
func (r *Request) Clone() Request {
	return Request{
		Method:  r.Method,
		URL:     r.URL,
		Query:   r.Query,
		Path:    r.Path,
		Body:    r.Body,
		Params:  r.Params,
		Headers: r.Headers,

		RemoteAddr: r.RemoteAddr,
	}
}

// CloneRequest returns a deep copy of the received request, so the received and the
// returned proxy.Request do not share a pointer
func CloneRequest(r *Request) *Request {
	clone := r.Clone()
	clone.Headers = CloneRequestHeaders(r.Headers)
	clone.Params = CloneRequestParams(r.Params)
	if r.Body == nil {
		return &clone
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(r.Body)
	r.Body.Close()

	r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	clone.Body = io.NopCloser(buf)

	return &clone
}

// CloneRequestHeaders returns a copy of the received request headers
func CloneRequestHeaders(headers map[string][]string) map[string][]string {
	m := make(map[string][]string, len(headers))
	for k, vs := range headers {
		tmp := make([]string, len(vs))
		copy(tmp, vs)
		m[k] = tmp
	}
	return m
}

// CloneRequestParams returns a copy of the received request params
func CloneRequestParams(params map[string]string) map[string]string {
	m := make(map[string]string, len(params))
	for k, v := range params {
		m[k] = v
	}
	return m
}

// HeaderGet get key from request headers.
func (r *Request) HeaderGet(key string) string {
	return textproto.MIMEHeader(r.Headers).Get(key)
}

/********* Response Implement ResponseWriter interface *********/

func (resp *Response) Header() http.Header {
	return resp.Metadata.Headers
}

func (resp *Response) Write(b []byte) (int, error) {
	data := map[string]interface{}{}
	if err := json.Unmarshal(b, &data); err != nil {
		return 0, err
	}
	resp.Data = data
	return len(b), nil
}

func (resp *Response) WriteHeader(statusCode int) {
	resp.Metadata.StatusCode = statusCode
}

// ModifyHeader 将Response的HTTP头写入目标头中.
func (resp *Response) ModifyHeader(c *gin.Context) {
	for key, values := range resp.Metadata.Headers {
		if key == "Content-Length" {
			continue
		}
		for _, v := range values {
			var exist bool
			for _, elem := range c.Writer.Header()[key] {
				if v == elem {
					exist = true
					break
				}
			}
			if exist {
				continue
			}
			c.Writer.Header().Add(key, v)
		}
	}
	c.Status(resp.Metadata.StatusCode)
}
