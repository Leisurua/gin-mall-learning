package httpapi

import "github.com/gin-gonic/gin"

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Data  any       `json:"data"`
	Error *APIError `json:"error"`
}

func success(c *gin.Context, data any) {
	c.JSON(200, response{Data: data})
}

func failure(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, response{
		Error: &APIError{Code: code, Message: message},
	})
}
