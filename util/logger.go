package util

import (
	"log"
	"os"
)

var (
	Logger = log.New(os.Stdout, "[serverless]", log.LstdFlags)
)

func Info(msg string, args ...interface{}) {
	Logger.Printf(msg, args...)
}
