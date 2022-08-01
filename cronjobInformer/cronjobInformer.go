package cronjobInformer

import (
	"fmt"
	"log"
	"time"

	batchbeta1 "k8s.io/api/batch/v1beta1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

func WatchCronjobPods(cs *kubernetes.Clientset) error {
	informersFactory := informers.NewSharedInformerFactory(cs, time.Second*30)
	cronjobInformer := informersFactory.Batch().V1beta1().CronJobs()

	cronjobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cronjob := obj.(*batchbeta1.CronJob)
			c := AddSidecarQuitScript(cronjob)
			log.Printf("%v\n", c)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {},
		DeleteFunc: func(obj interface{}) {},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	// start cronjobInformer
	informersFactory.Start(stopCh)
	informersFactory.WaitForCacheSync(stopCh)

	<-stopCh

	return nil

}

func AddSidecarQuitScript(j *batchbeta1.CronJob) *batchbeta1.CronJob {

	sidecarQuitCmd := fmt.Sprintf(`
for i in {1..5};do
  echo $i
done
`)

	var appCommand []string
	var appArgs []string

	for _, c := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			if c.Command != nil {
				appCommand = append(appCommand, c.Command...)
			}

			if c.Args != nil {
				appArgs = append(appArgs, c.Args...)
			}
		}
	}

	newCmd := []string{
		"/bin/sh",
		"-c",
		sidecarQuitCmd,
	}

	newCmd = append(newCmd, appCommand...)
	newCmd = append(newCmd, appArgs...)

	for _, v := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if v.Name == "app" {
			v.Command = newCmd
		}
	}

	fmt.Printf("print newCmd = %v\n", newCmd)
	return j
}
