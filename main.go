package main

import (
	"go-serverless/manager"
)

func main() {
	if err := manager.LoadAllFunctions("functions"); err != nil {
		panic(err)
	}
	r := SetupRouter()
	r.Run(":8080")
}
