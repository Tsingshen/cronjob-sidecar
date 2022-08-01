package main

import (
	"cronjob-sidecar/cronjobInformer"

	k8sClient "github.com/Tsingshen/k8scrd/client"
)

func main() {

	cs := k8sClient.GetClient()

	err := cronjobInformer.WatchCronjobPods(cs)

	if err != nil {
		// fmt.Printf("watchCronjobs err: %s\n", err)
		panic(err)
	}

}
