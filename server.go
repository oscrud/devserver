package devserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"

	"github.com/bxcodec/faker/v3"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
)

// devServer :
type devServer struct {
	Password string
	Handler  interface{}
}

type devRequest struct {
	Endpoint string            `json:"endpoint"`
	Request  json.RawMessage   `json:"request"`
	Metadata map[string]string `json:"metadata"`
}

type devRequestSpec struct {
	Endpoint string `json:"endpoint"`
	Request  string `json:"request"`
}

// Start :
func Start(port string, password string, handler interface{}, server *grpc.Server) {
	listen, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf(ErrFailToListen, err)
	}

	log.Println("➸  grpc, gdev server started on :" + port)
	m := cmux.New(listen)
	grpcListen := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	httpListen := m.Match(cmux.HTTP1Fast())
	go server.Serve(grpcListen)
	go http.Serve(
		httpListen, devServer{
			Password: password,
			Handler:  handler,
		},
	)
	log.Fatal(m.Serve())
}

// ServeHTTP :
func (dev devServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, x-oscrud-dev")

	if dev.Password != "" {
		password := r.Header.Get("x-oscrud-dev")
		if password == "" || password != dev.Password {
			w.Write([]byte(ErrInvalidPassword))
			return
		}
	}

	switch r.Method {

	case "GET":
		specs := make([]devRequestSpec, 0)

		rValue := reflect.ValueOf(dev.Handler)
		rType := reflect.TypeOf(dev.Handler)
		for i := 0; i < rValue.NumMethod(); i++ {
			field := rType.Method(i)
			name := field.Name

			requestType := field.Func.Type().In(2)
			requestValue := reflect.New(requestType)
			requestSpec := requestValue.Interface()
			faker.SetRandomStringLength(1)
			faker.FakeData(requestSpec)

			bytes, _ := json.Marshal(requestSpec)
			specs = append(specs, devRequestSpec{
				Endpoint: name,
				Request:  string(bytes),
			})
		}

		bytes, _ := json.Marshal(specs)
		w.Write(bytes)
		break

	case "POST":
		bytes, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.Write([]byte(fmt.Sprintf(ErrBodyReadFail, err.Error())))
			return
		}

		req := new(devRequest)
		if err := json.Unmarshal(bytes, req); err != nil {
			w.Write([]byte(fmt.Sprintf(ErrInvalidJsonRequest, err.Error())))
			return
		}

		rValue := reflect.ValueOf(dev.Handler)
		method := rValue.MethodByName(req.Endpoint)

		params := make([]reflect.Value, 0)
		params = append(params, reflect.ValueOf(context.Background()))

		reqType := method.Type().In(1)
		reqValue := reflect.New(reqType.Elem()).Interface()

		if err := json.Unmarshal(req.Request, reqValue); err != nil {
			w.Write([]byte(err.Error()))
			return
		}

		params = append(params, reflect.ValueOf(reqValue))
		returns := method.Call(params)
		if !returns[1].IsNil() {
			err := returns[1].Interface().(error)
			w.Write([]byte(strings.ReplaceAll(err.Error(), "rpc error: code = Unknown desc = ", "")))
			return
		}

		if !returns[0].CanInterface() {
			w.Write([]byte("Error: response can't be interface, result possible be null or empty"))
			return
		}

		bytes, err = json.Marshal(returns[0].Interface())
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		w.Write(bytes)
		break

	case "OPTIONS":
		break

	default:
		w.Write([]byte("method not allowed"))
		break

	}
}
