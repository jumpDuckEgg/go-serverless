package handler

import (
	"github.com/gin-gonic/gin"
	"go-serverless/manager"
	"net/http"
)

func InvokeFunction(c *gin.Context) {
	id := c.Param("id")
	input := c.PostForm("input")
	result, err := manager.InvokeFunction(id, input)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
