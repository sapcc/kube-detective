package main

import (
	"github.com/sapcc/kube-detective/cmd/internal/cmd"
	"os"
)

func main(){
	err := cmd.RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

