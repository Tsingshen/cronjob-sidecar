package cronjobInformer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	batchbeta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

func WatchCronjobs(cs *kubernetes.Clientset) error {
	informersFactory := informers.NewSharedInformerFactory(cs, time.Second*30)
	cronjobInformer := informersFactory.Batch().V1beta1().CronJobs()

	cronjobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cronjob := obj.(*batchbeta1.CronJob)
			templateAnno := cronjob.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations
			if cronjob.Namespace == "beta1" {
				if templateAnno != nil {
					if !checkAnno(cronjob.Annotations, "app.lzwk.com/sidecar-cronjob", "yes") {
						if templateAnno["sidecar.istio.io/inject"] == "true" {
							c, changed := AddSidecarQuitScript(cronjob)
							if changed {
								err := updateCronjob(cs, c)
								if err != nil {
									log.Printf("updateCronjob error: %v\n", err)
								}
							}
						}
					}
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCronjob := oldObj.(*batchbeta1.CronJob)
			newCronjob := newObj.(*batchbeta1.CronJob)

			oldAnno := oldCronjob.Annotations
			newAnno := newCronjob.Annotations

			oldTempAnno := oldCronjob.Spec.JobTemplate.Spec.Template.Annotations
			newTempAnno := newCronjob.Spec.JobTemplate.Spec.Template.Annotations

			if oldCronjob.Namespace == "beta1" {
				if !checkAnno(oldAnno, "app.lzwk.com/sidecar-cronjob", "yes") && !checkAnno(newAnno, "app.lzwk.com/sidecar-cronjob", "yes") {
					if checkSidecarInject(oldTempAnno, newTempAnno) {
						c, changed := AddSidecarQuitScript(newCronjob)
						if changed {
							err := updateCronjob(cs, c)
							if err != nil {
								log.Printf("updateCronjob error: %v\n", err)
							}
						}
					}
				}

			}

		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	// start cronjobInformer
	informersFactory.Start(stopCh)
	informersFactory.WaitForCacheSync(stopCh)

	<-stopCh

	return nil

}

func checkSidecarInject(oldTempAnno, newTempAnno map[string]string) bool {

	if newTempAnno == nil {
		return false
	}

	if newTempAnno["sidecar.istio.io/inject"] == "true" {
		if oldTempAnno == nil {
			return true
		}

		if oldTempAnno["sidecar.istio.io/inject"] == "false" {
			return true
		}
	}

	return false
}

func checkAnno(anno map[string]string, key, value string) bool {

	if anno == nil {
		return false
	}

	if anno[key] == value {
		return true
	}

	return false
}

func AddSidecarQuitScript(j *batchbeta1.CronJob) (*batchbeta1.CronJob, bool) {

	anno := j.Annotations

	if anno != nil {
		return j, false
	}

	sidecarQuitCmd := `trap "curl --max-time 2 -sS -f -XPOST http://127.0.0.1:15000/quitquitquit" EXIT;while ! curl -s -f http://127.0.0.1:15021/healthz/ready;do sleep 1;done;sleep 2`

	var appCommand string
	var appArgs string

	for _, c := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			if c.Command != nil {
				appCommand = strings.Join(c.Command, " ")
				sidecarQuitCmd = sidecarQuitCmd + ";" + appCommand
			}
			if c.Args != nil {
				appArgs = strings.Join(c.Args, " ")
				sidecarQuitCmd = sidecarQuitCmd + ";" + appArgs
			}
		}
	}

	newCmd := []string{
		"/bin/sh",
		"-c",
		sidecarQuitCmd,
	}

	for k, v := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if v.Name == "app" {
			j.Spec.JobTemplate.Spec.Template.Spec.Containers[k].Command = newCmd
			j.Spec.JobTemplate.Spec.Template.Spec.Containers[k].Args = nil

			if anno == nil {
				anno = make(map[string]string)
			}

			anno["app.lzwk.com/sidecar-cronjob"] = "yes"
		}
	}

	log.Printf("Add cronjob = %s.%s sidecar quit script\n", j.Namespace, j.Name)

	return j, true
}

func updateCronjob(cs *kubernetes.Clientset, c *batchbeta1.CronJob) error {
	if c != nil {

		_, err := cs.BatchV1beta1().CronJobs(c.Namespace).Update(context.Background(), c, metav1.UpdateOptions{})

		if err != nil {
			return err
		}
		log.Printf("Updated cronjob = %s.%s completed\n", c.Namespace, c.Name)
		return nil

	}

	return fmt.Errorf("updateCronjob, args == nil, please check")

}
