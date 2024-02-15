package main

import (
	pb "github.com/clydotron/go-micro-auth-service/protos"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"time"

	helpers "github.com/clydotron/toolbox/helpers"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type jsonResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// TODO use oneof
type requestPayload struct {
	Action string      `json:"action"`
	Auth   AuthPayload `json:"auth,omitempty"`
	Log    LogPayload  `json:"log,omitempty"`
}

type AuthPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LogPayload struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

const (
	authServiceURL   = "http://auth-service/authenticate"
	logServiceURL    = "http://log-service/log"
	logServiceRPCURL = "log-service:5001"
	logServiceRPC    = "RPCServer.LogInfo"
)

func (app *App) Broker(w http.ResponseWriter, r *http.Request) {
	log.Println("Hit the broker route.")
	payload := jsonResponse{
		Error:   false,
		Message: "hit the broker",
	}

	out, _ := json.MarshalIndent(payload, "", "\t")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write(out)
}

func (app *App) HandleSubmission(w http.ResponseWriter, r *http.Request) {
	var payload requestPayload
	log.Println("HandleSubmission >>>>")
	if err := helpers.ReadJSON(w, r, &payload); err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	log.Println("HandleSubmission: ", payload.Action)
	switch payload.Action {
	case "auth":
		//app.authenticate(w, payload.Auth)
		app.authenticateViaGRPC(w, payload.Auth)

	case "log":
		//app.logItem(w, payload.Log)
		app.logItemRPC(w, payload.Log)
	default:
		helpers.ErrorJSON(w, errors.New("unknown action"))
	}

}

func sendResponse(w http.ResponseWriter, err bool, msg string, data any) {
	payload := jsonResponse{
		Error:   err,
		Message: msg,
		Data:    data,
	}
	helpers.WriteJSON(w, http.StatusAccepted, payload)
}

func (app *App) authenticate(w http.ResponseWriter, auth AuthPayload) {
	// send request to the authentication service:
	jsonData, _ := json.MarshalIndent(auth, "", "\t") //TODO remove before prod
	request, err := http.NewRequest("POST", authServiceURL, bytes.NewBuffer(jsonData))
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}
	defer response.Body.Close()

	// make sure we get back the correct status code
	switch response.StatusCode {
	case http.StatusUnauthorized:
		helpers.ErrorJSON(w, errors.New("invalid credentials"))
		return
	case http.StatusAccepted:
	default:
		helpers.ErrorJSON(w, errors.New("error calling auth service"))
		return
	}

	// process the response from the auth service:
	var jsonFromService jsonResponse

	// decode the json from the auth service
	if err = json.NewDecoder(response.Body).Decode(&jsonFromService); err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	if jsonFromService.Error {
		helpers.ErrorJSON(w, err, http.StatusUnauthorized)
		return
	}

	// send message to the log service
	_, err = app.logRequestRPC("authentication", fmt.Sprintf("%s successfully logged in", auth.Email))
	if err != nil {
		fmt.Printf("Error logging auth status:%v\n", err)
	}

	sendResponse(w, false, "Authenticated", jsonFromService.Data)
}

// not used. remove shortly
func (app *App) logItem(w http.ResponseWriter, entry LogPayload) {
	jsonData, err := json.Marshal(entry)
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	request, err := http.NewRequest("POST", logServiceURL, bytes.NewBuffer(jsonData))
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		log.Println("Failed:", response.StatusCode)
		helpers.ErrorJSON(w, fmt.Errorf("failed to contact log service: %d", response.StatusCode))
		return
	}

	sendResponse(w, false, "logged", nil)
}

type RPCPayload struct {
	Name string
	Data string
}

func (app *App) logItemRPC(w http.ResponseWriter, entry LogPayload) {
	result, err := app.logRequestRPC(entry.Name, entry.Data)
	if err != nil {
		helpers.ErrorJSON(w, err)
	}

	sendResponse(w, false, result, nil)
}

func (app *App) logRequestRPC(name, data string) (string, error) {
	client, err := rpc.Dial("tcp", logServiceRPCURL)
	if err != nil {
		return "", err
	}

	rpcPayload := RPCPayload{
		Name: name,
		Data: data,
	}

	var result string
	if err = client.Call(logServiceRPC, rpcPayload, &result); err != nil {
		return "", err
	}

	return result, nil
}

func (app *App) authenticateViaGRPC(w http.ResponseWriter, auth AuthPayload) {
	conn, err := grpc.Dial("auth-service:50001", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}
	defer conn.Close()

	c := pb.NewAuthServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	authResponse, err := c.Authenticate(ctx, &pb.AuthRequest{Email: auth.Email, Password: auth.Password})
	if err != nil {
		helpers.ErrorJSON(w, err)
		return
	}

	if authResponse.GetResult() == "Authenticated" {
		// send message to the log service
		_, err = app.logRequestRPC("authentication", fmt.Sprintf("%s successfully logged in", auth.Email))
		if err != nil {
			fmt.Printf("Error logging auth status:%v\n", err)
		}
	}
	// what about failures?

	sendResponse(w, false, authResponse.GetResult(), nil)
}
