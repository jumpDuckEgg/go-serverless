package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin error:", err)
		os.Exit(1)
	}
	fmt.Print("收到全部输入：", string(data))
	fmt.Println("Hello, serverless!")
}
