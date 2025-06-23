package main

import (
	"github.com/gin-gonic/gin"
	"go-serverless/handler"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()
	r.POST("/functions", handler.RegisterFunction)
	r.GET("/functions", handler.ListFunctions)
	r.GET("/functions/:id", handler.GetFunction)
	r.DELETE("/functions/:id", handler.DeleteFunction)
	r.POST("/invoke/:id", handler.InvokeFunction)
	return r
}
