package cronjobinformer

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
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
	watchNs := os.Getenv("WATCH_NS")
	nsReg := regexp.MustCompile(`shencq`)
	if watchNs != "" {
		nsReg = regexp.MustCompile(watchNs)
	}

	cronjobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cronjob := obj.(*batchbeta1.CronJob)
			templateAnno := cronjob.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations

			containters := cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers

			// check quitquitquit exist
			var cmd string
			var matchStr = "quitquitquit"

			for _, c := range containters {
				if c.Name == "app" {
					cmd = strings.Join(c.Command, " ")
				}
			}

			if templateAnno != nil {
				if templateAnno["sidecar.istio.io/inject"] == "true" {
					if !checkCmd(cmd, matchStr) {
						if nsReg.MatchString(cronjob.Namespace) {
							log.Printf("INFO: AddFunc add cronjob %s.%s\n", cronjob.Namespace, cronjob.Name)
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

			oldTempAnno := oldCronjob.Spec.JobTemplate.Spec.Template.Annotations
			newTempAnno := newCronjob.Spec.JobTemplate.Spec.Template.Annotations

			oldC := oldCronjob.Spec.JobTemplate.Spec.Template.Spec.Containers
			newC := newCronjob.Spec.JobTemplate.Spec.Template.Spec.Containers

			var oldCmd string
			var newCmd string
			var matchStr = "quitquitquit"

			for _, c := range oldC {
				if c.Name == "app" {
					oldCmd = strings.Join(c.Command, " ")
					if c.Args != nil {
						oldCmd = oldCmd + " " + strings.Join(c.Args, " ")
					}
				}
			}

			for _, c := range newC {
				if c.Name == "app" {
					newCmd = strings.Join(c.Command, " ")
					if c.Args != nil {
						newCmd = newCmd + " " + strings.Join(c.Args, " ")
					}
				}
			}

			// add sidecar quit script
			if !checkCmd(oldCmd, matchStr) && !checkCmd(newCmd, matchStr) {
				if checkSidecarInject(oldTempAnno, newTempAnno) {
					if nsReg.MatchString(oldCronjob.Namespace) {
						log.Printf("INFO: UpdateFunc add cronjob sidecar quit script %s.%s\n", oldCronjob.Namespace, oldCronjob.Name)
						c, changed := AddSidecarQuitScript(newCronjob)
						if changed {
							err := updateCronjob(cs, c)
							if err != nil {
								log.Printf("update cronjob error: %v\n", err)
							}
						}
					}
				}
			}

			// remove sidecar quit script
			if checkCmd(oldCmd, matchStr) && checkCmd(newCmd, matchStr) {
				if checkSidecarUnInject(oldTempAnno, newTempAnno) {
					if nsReg.MatchString(oldCronjob.Namespace) {
						log.Printf("INFO: Remove cronjob sidecar quit script %s.%s\n", oldCronjob.Namespace, oldCronjob.Name)
						c, ok := RemoveSidecarQuitScript(newCronjob)
						if ok {
							err := updateCronjob(cs, c)
							if err != nil {
								log.Printf("update cronjob error: %v\n", err)
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

func checkSidecarUnInject(oldTempAnno, newTempAnno map[string]string) bool {

	if newTempAnno == nil {
		return false
	}

	if newTempAnno["sidecar.istio.io/inject"] == "false" {
		if oldTempAnno == nil || oldTempAnno["sidecar.istio.io/inject"] == "true" {
			return true
		}

	}

	return false
}

func checkSidecarInject(oldTempAnno, newTempAnno map[string]string) bool {

	if newTempAnno == nil {
		return false
	}

	if newTempAnno["sidecar.istio.io/inject"] == "true" {
		if oldTempAnno == nil || oldTempAnno["sidecar.istio.io/inject"] == "false" {
			return true
		}

	}

	return false
}

func checkCmd(cmd string, str string) bool {

	if cmd == "" {
		return false
	}

	reg := regexp.MustCompile(str)
	return reg.MatchString(cmd)

}

func RemoveSidecarQuitScript(j *batchbeta1.CronJob) (*batchbeta1.CronJob, bool) {

	sidecarQuitCmd := `trap "curl --max-time 2 -sS -f -XPOST http://127.0.0.1:15000/quitquitquit" EXIT;while ! curl -s -f http://127.0.0.1:15021/healthz/ready;do sleep 1;done;sleep 2`

	for _, c := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			if c.Command != nil && c.Args == nil {
				if len(c.Command) > 2 {
					if c.Command[0] == "/bin/bash" && c.Command[1] == "-c" ||
						c.Command[0] == "/bin/sh" && c.Command[1] == "-c" ||
						c.Command[0] == "sh" && c.Command[1] == "-c" {

						_, b, ok := strings.Cut(c.Command[2], sidecarQuitCmd+";")

						if ok {
							c.Command[2] = b
							log.Printf("Remove cronjob = %s.%s sidecar quit script\n", j.Namespace, j.Name)
							return j, true
						}
					}
				}
			}
		}
	}
	return nil, false

}

func AddSidecarQuitScript(j *batchbeta1.CronJob) (*batchbeta1.CronJob, bool) {

	const sidecarQuitCmd = `trap "curl --max-time 2 -sS -f -XPOST http://127.0.0.1:15000/quitquitquit" EXIT;while ! curl -s -f http://127.0.0.1:15021/healthz/ready;do sleep 1;done;sleep 2`

	newCmd := []string{}

	for _, c := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			if c.Command != nil && c.Args != nil {
				if len(c.Command) >= 2 {
					if c.Command[0] == "/bin/bash" && c.Command[1] == "-c" ||
						c.Command[0] == "/bin/sh" && c.Command[1] == "-c" ||
						c.Command[0] == "sh" && c.Command[1] == "-c" {
						appArgs := strings.Join(c.Args, " ")
						if len(c.Command) == 2 {
							finalCmd := sidecarQuitCmd + ";" + appArgs
							c.Command = append(c.Command, finalCmd)
							newCmd = c.Command
						} else {
							c.Command[2] = sidecarQuitCmd + ";" + strings.Join(c.Command[2:], " ") + " " + appArgs
							newCmd = c.Command[:3]
						}
					} else {
						newCmd = []string{
							"/bin/sh",
							"-c",
							sidecarQuitCmd + ";" + strings.Join(c.Command, " "),
						}
					}
				} else {
					appCommand := strings.Join(c.Command, " ")
					appArgs := strings.Join(c.Args, " ")
					quitCmdStr := sidecarQuitCmd + ";" + appCommand + " " + appArgs
					newCmd = []string{
						"/bin/sh",
						"-c",
						quitCmdStr,
					}
				}
				break
			}

			if c.Command != nil && c.Args == nil {
				if len(c.Command) >= 2 {
					if c.Command[0] == "/bin/bash" && c.Command[1] == "-c" ||
						c.Command[0] == "/bin/sh" && c.Command[1] == "-c" ||
						c.Command[0] == "sh" && c.Command[1] == "-c" {
						if len(c.Command) == 2 {
							newCmd = c.Command
						} else {
							c.Command[2] = sidecarQuitCmd + ";" + strings.Join(c.Command[2:], " ")
							newCmd = c.Command[:3]
						}
					} else {
						newCmd = []string{
							"/bin/sh",
							"-c",
							sidecarQuitCmd + ";" + strings.Join(c.Command, " "),
						}
					}
				} else {
					appCommand := strings.Join(c.Command, " ")
					quitCmdStr := sidecarQuitCmd + ";" + appCommand
					newCmd = []string{
						"/bin/sh",
						"-c",
						quitCmdStr,
					}
				}
				break
			}

			if c.Command == nil && c.Args != nil {
				appArgs := strings.Join(c.Args, " ")
				quitCmdStr := sidecarQuitCmd + ";" + appArgs
				newCmd = []string{
					"/bin/sh",
					"-c",
					quitCmdStr,
				}
				break
			}

			if c.Command == nil && c.Args == nil {

				log.Printf("Do nothing, cronjob = %s.%s no command and args\n", j.Namespace, j.Name)
				return j, true
			}

		}
	}

	for k, v := range j.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if v.Name == "app" {
			j.Spec.JobTemplate.Spec.Template.Spec.Containers[k].Command = newCmd
			// args have been added into commands
			j.Spec.JobTemplate.Spec.Template.Spec.Containers[k].Args = nil
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
