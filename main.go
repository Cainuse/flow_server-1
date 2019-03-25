package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SermoDigital/jose"
	"github.com/gin-gonic/gin"
	"github.com/golang/protobuf/jsonpb"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"google.golang.org/genproto/googleapis/cloud/dialogflow/v2"
)

type Request struct {
	Email  string `json:"email"`
	Action string `json:"action"`
}

type myStruct struct {
	Intent     string           `json:"command"`
	Parameter  *structpb.Struct `json:"param"`
	Message    string
	Connection *websocket.Conn
}

type JwtHeaderStruct struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type JwtBodyStruct struct {
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	Emailverified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Iat           int    `json:"iat"`
	Exp           int    `json:"exp"`
	Jti           string `json:"jti"`
}

/*
Webhook handler, handle all request from Dialogflow. Obtain message,
parse JWT tokens and parse commands into Struct objects. Passes result
to wsHandler.
*/
func handleWebhook(c *gin.Context, connections *map[string]*myStruct) {
	var err error
	var unmar jsonpb.Unmarshaler
	unmar.AllowUnknownFields = true

	//Parsing request
	wr := dialogflow.WebhookRequest{}

	if err = unmar.Unmarshal(c.Request.Body, &wr); err != nil {
		logrus.WithError(err).Error("Couldn't Unmarshal request to jsonpb")
		c.Status(http.StatusBadRequest)
		return
	}

	userQueryText := wr.GetQueryResult().GetQueryText()
	queryAction := wr.GetQueryResult().GetAction()
	queryParameters := wr.GetQueryResult().GetParameters()
	queryIntent := wr.GetQueryResult().GetIntent().GetDisplayName()
	queryUserID := wr.GetOriginalDetectIntentRequest().GetPayload().GetFields()["user"].GetStructValue().GetFields()["userId"]

	//Obtain IdToken
	// Request information extraction based on json request
	tokenPayloadResponse := wr.GetOriginalDetectIntentRequest().GetPayload().GetFields()
	userStruct := tokenPayloadResponse["user"]
	idTokenString := userStruct.GetStructValue().GetFields()["idToken"].GetStringValue()
	splitJWT := strings.Split(idTokenString, ".")
	jwtHeader, _ := (jose.Base64Decode([]byte(splitJWT[0])))
	jwtBody, _ := (jose.Base64Decode([]byte(splitJWT[1])))

	//jwtSigniture, _ := (jose.Base64Decode([]byte(splitJWT[2])))

	//Debug logs... To be deleted

	fmt.Println(userQueryText, queryAction, queryParameters, queryIntent, queryUserID)

	headerStruct := JwtHeaderStruct{}
	bodyStruct := JwtBodyStruct{}
	headerError := json.Unmarshal(jwtHeader, &headerStruct)
	bodyError := json.Unmarshal(jwtBody, &bodyStruct)

	// If any error happens it is to be caught here
	if headerError != nil && bodyError != nil {
		fmt.Println("Error in parsing bodies")
	}

	con := *connections

	//Connection not yet to be created
	if con[bodyStruct.Email] == nil {
		fmt.Println("Creating connection, Webhook")
		con[bodyStruct.Email] = &myStruct{
			Intent:    queryIntent,
			Parameter: queryParameters}

	} else {
		fmt.Println("Connection found writing to JSON, Webhook")
		payload := *con[bodyStruct.Email]
		//Update myStruct to have the command from dialogflow
		payload.Intent = queryIntent
		payload.Parameter = queryParameters
		//Send myStruct to websocket
		fmt.Println("ADDED CMD TO CHANNEL")
		payload.Connection.WriteJSON(payload)

	}

}

// Upgrades connection from HTTP to websocket
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

/*
Websocket handler. Handles all client side message logic, parsing
all client messages and establish user profile in server.
*/
func wshandler(w http.ResponseWriter, r *http.Request, connections *map[string]*myStruct) {
	var conn, err = upgrader.Upgrade(w, r, nil)

	if err != nil {
		fmt.Println("Failed to set websocket upgrade: %+v", err)
		return
	} else {
		conn.WriteJSON(myStruct{
			Message: "Connection successfully established"})
	}

	// Handles incoming messages to server
	go func(conn *websocket.Conn) {
		_, p, err := conn.ReadMessage()
		fmt.Println(string(p))
		if err != nil {
			conn.Close()
		} else {

			request := Request{}
			error := json.Unmarshal(p, &request)

			if error != nil {
				fmt.Println(error)
			}

			if (len(request.Email) > 0) && (request.Action == "Sign In") {
				fmt.Println("Channel Created!")
				connec := *connections
				//If user already exists
				if connec[request.Email] == nil {
					fmt.Println("New Connection Created! Websocket")
					connec[request.Email] = &myStruct{
						Connection: conn}
				} else {
					fmt.Println("Connection found, adding conn Websocket")
					payload := *connec[request.Email]
					payload.Connection = conn
				}
			}

			// After email is registered use go routine's channel to receive message from webhook result
		}

	}(conn)
	fmt.Println("Webhook reached")
}

/*
Application entry point, creates Websocket endpoint & Webhook endpoint*/
func main() {
	var err error
	r := gin.Default()
	connections := make(map[string]*myStruct)

	// Endpoint for webhook connection to dialogflow
	r.POST("/dialogflow", func(c *gin.Context) {
		handleWebhook(c, &connections)
	})

	// Endpoint for websocket connection
	r.GET("/ws", func(c *gin.Context) {
		wshandler(c.Writer, c.Request, &connections)
	})
	// Blocking function
	if err = r.Run("localhost:9090"); err != nil {
		logrus.WithError(err).Fatal("Couldn't start server")
	}
}
