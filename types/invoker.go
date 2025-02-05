// Copyright (c) OpenFaaS Author(s) 2019. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package types

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/openfaas/faas-provider/auth"
	"github.com/pkg/errors"
)

type Invoker struct {
	PrintResponse bool
	Client        *http.Client
	GatewayURL    string
	Responses     chan InvokerResponse
	Credentials   *auth.BasicAuthCredentials
}

type InvokerResponse struct {
	Context  context.Context
	Body     *[]byte
	Header   *http.Header
	Status   int
	Error    error
	Topic    string
	Function string
}

func NewInvoker(gatewayURL string, client *http.Client, printResponse bool, credentials *auth.BasicAuthCredentials) *Invoker {
	return &Invoker{
		PrintResponse: printResponse,
		Client:        client,
		GatewayURL:    gatewayURL,
		Responses:     make(chan InvokerResponse),
		Credentials:   credentials,
	}
}

// Invoke triggers a function by accessing the API Gateway
func (i *Invoker) Invoke(topicMap *TopicMap, topic string, message *[]byte) {
	i.InvokeWithContext(context.Background(), topicMap, topic, message)
}

//InvokeWithContext triggers a function by accessing the API Gateway while propagating context
func (i *Invoker) InvokeWithContext(ctx context.Context, topicMap *TopicMap, topic string, message *[]byte) {
	if len(*message) == 0 {
		i.Responses <- InvokerResponse{
			Context: ctx,
			Error:   fmt.Errorf("no message to send"),
		}
	}

	matchedFunctions := topicMap.Match(topic)
	for _, matchedFunction := range matchedFunctions {
		log.Printf("Invoke function: %s", matchedFunction)

		gwURL := fmt.Sprintf("%s/%s", i.GatewayURL, matchedFunction)
		reader := bytes.NewReader(*message)

		body, statusCode, header, doErr := invokefunction(ctx, i.Client, gwURL, reader, i.Credentials)

		if doErr != nil {
			i.Responses <- InvokerResponse{
				Context: ctx,
				Error:   errors.Wrap(doErr, fmt.Sprintf("unable to invoke %s", matchedFunction)),
			}
			continue
		}

		i.Responses <- InvokerResponse{
			Context:  ctx,
			Body:     body,
			Status:   statusCode,
			Header:   header,
			Function: matchedFunction,
			Topic:    topic,
		}
	}
}

func invokefunction(ctx context.Context, c *http.Client, gwURL string, reader io.Reader, cred *auth.BasicAuthCredentials) (*[]byte, int, *http.Header, error) {

	httpReq, _ := http.NewRequest(http.MethodPost, gwURL, reader)
	httpReq.WithContext(ctx)
	if cred != nil {
		httpReq.SetBasicAuth(cred.User, cred.Password)

	}
	if httpReq.Body != nil {
		defer httpReq.Body.Close()
	}

	var body *[]byte

	res, doErr := c.Do(httpReq)
	if doErr != nil {
		return nil, http.StatusServiceUnavailable, nil, doErr
	}

	if res.Body != nil {
		defer res.Body.Close()

		bytesOut, readErr := ioutil.ReadAll(res.Body)
		if readErr != nil {
			log.Printf("Error reading body")
			return nil, http.StatusServiceUnavailable, nil, doErr

		}
		body = &bytesOut
	}

	return body, res.StatusCode, &res.Header, doErr
}
