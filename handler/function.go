package handler

import (
	"fmt"
	"go-serverless/manager"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterFunction(c *gin.Context) {
	name := c.PostForm("name")
	version := c.PostForm("version")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}

	file, err := fileHeader.Open()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open file failed"})
		return
	}

	defer file.Close()

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))

	if version == "" {
		version = fmt.Sprintf("v%d", time.Now().Unix())
	}

	fn, err := manager.RegisterFunction(name, file, version, ext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, fn)
}

func ListFunctions(c *gin.Context) {
	fns := manager.ListFunctions()
	c.JSON(http.StatusOK, fns)
}

func GetFunction(c *gin.Context) {
	id := c.Param("id")
	fn, err := manager.GetFunction(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, fn)
}

func DeleteFunction(c *gin.Context) {
	id := c.Param("id")
	err := manager.DeleteFunction(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "true"})
}
