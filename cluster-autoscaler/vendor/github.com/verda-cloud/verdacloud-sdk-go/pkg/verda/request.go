package verda

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type Response struct {
	*http.Response
}

func getRequest[T any](ctx context.Context, client *Client, url string) (T, *Response, error) {
	var respBody T

	req, err := client.NewRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return respBody, nil, err
	}

	resp, err := client.Do(req, &respBody)
	if err != nil {
		return respBody, resp, err
	}

	return respBody, resp, nil
}

func requestWithBody[T any](ctx context.Context, client *Client, method, url string, reqBody any) (T, *Response, error) {
	var respBody T

	var reqBodyReader io.Reader
	if reqBody != nil {
		reqBodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return respBody, nil, err
		}

		reqBodyReader = bytes.NewReader(reqBodyBytes)
	}

	req, err := client.NewRequest(ctx, method, url, reqBodyReader)
	if err != nil {
		return respBody, nil, err
	}

	resp, err := client.Do(req, &respBody)
	if err != nil {
		return respBody, resp, err
	}

	return respBody, resp, nil
}

func postRequest[T any](ctx context.Context, client *Client, url string, reqBody any) (T, *Response, error) {
	return requestWithBody[T](ctx, client, http.MethodPost, url, reqBody)
}

//nolint:unused // Reserved for POST when response body is empty or not needed
func postRequestAllowEmptyResponse(ctx context.Context, client *Client, url string, reqBody any) (*Response, error) {
	var reqBodyReader io.Reader
	if reqBody != nil {
		reqBodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		reqBodyReader = bytes.NewReader(reqBodyBytes)
	}

	req, err := client.NewRequest(ctx, http.MethodPost, url, reqBodyReader)
	if err != nil {
		return nil, err
	}

	return client.Do(req, nil)
}

func putRequest[T any](ctx context.Context, client *Client, url string, reqBody any) (T, *Response, error) {
	return requestWithBody[T](ctx, client, http.MethodPut, url, reqBody)
}

func putRequestAllowEmptyResponse(ctx context.Context, client *Client, url string, reqBody any) (*Response, error) {
	var reqBodyReader io.Reader
	if reqBody != nil {
		reqBodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		reqBodyReader = bytes.NewReader(reqBodyBytes)
	}

	req, err := client.NewRequest(ctx, http.MethodPut, url, reqBodyReader)
	if err != nil {
		return nil, err
	}

	return client.Do(req, nil)
}

func patchRequest[T any](ctx context.Context, client *Client, url string, reqBody any) (T, *Response, error) {
	return requestWithBody[T](ctx, client, http.MethodPatch, url, reqBody)
}

//nolint:unused // Reserved for DELETE endpoints with response bodies
func deleteRequest[T any](ctx context.Context, client *Client, url string) (T, *Response, error) {
	var respBody T

	req, err := client.NewRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return respBody, nil, err
	}

	resp, err := client.Do(req, &respBody)
	if err != nil {
		return respBody, resp, err
	}

	return respBody, resp, nil
}

func deleteRequestAllowEmptyResponse(ctx context.Context, client *Client, url string) (*Response, error) {
	req, err := client.NewRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	return client.Do(req, nil)
}

func deleteRequestWithBody(ctx context.Context, client *Client, url string, reqBody any) (*Response, error) {
	var reqBodyReader io.Reader
	if reqBody != nil {
		reqBodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		reqBodyReader = bytes.NewReader(reqBodyBytes)
	}

	req, err := client.NewRequest(ctx, http.MethodDelete, url, reqBodyReader)
	if err != nil {
		return nil, err
	}

	return client.Do(req, nil)
}
