package main

import (
	"cronjob-sidecar/cronjobinformer"
	"log"

	k8sClient "github.com/Tsingshen/k8scrd/client"
)

func main() {

	cs := k8sClient.GetClient()

	log.Println("Cronjob with sidecar watcher starts running")
	err := cronjobinformer.WatchCronjobs(cs)

	if err != nil {
		// fmt.Printf("watchCronjobs err: %s\n", err)
		panic(err)
	}

}
