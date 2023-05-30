/*
Copyright 2022 The Koordinator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduling

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"github.com/koordinator-sh/koordinator/test/e2e/framework"
	"github.com/koordinator-sh/koordinator/test/e2e/framework/manifest"
	e2enode "github.com/koordinator-sh/koordinator/test/e2e/framework/node"
)

var _ = SIGDescribe("PodTopologyAware", func() {
	f := framework.NewDefaultFramework("podtopologaware")
	var nodeList *corev1.NodeList

	ginkgo.BeforeEach(func() {
		nodeList = &corev1.NodeList{}
		var err error

		framework.AllNodesReady(f.ClientSet, time.Minute)

		nodeList, err = e2enode.GetReadySchedulableNodes(f.ClientSet)
		if err != nil {
			framework.Logf("Unexpected error occurred: %v", err)
		}
	})

	ginkgo.AfterEach(func() {

	})

	ginkgo.Context("Topology Aware functionality", func() {
		var testNodeNames []string
		fakeTopology := "fake-topology"

		ginkgo.BeforeEach(func() {
			if len(nodeList.Items) < 4 {
				ginkgo.Skip("At least 4 nodes are required to run the test")
			}

			ginkgo.By("Trying to get 4 available nodes which can run pod")
			testNodeNames = GetNNodesThatCanRunPod(f, 4)

			ginkgo.By(fmt.Sprintf("Apply topologyKey %v for this test on the 4 nodes.", fakeTopology))
			for i, nodeName := range testNodeNames {
				topology := "a"
				if float64(i+1)/float64(len(testNodeNames))*100 > 50 {
					topology = "b"
				}
				framework.AddOrUpdateLabelOnNode(f.ClientSet, nodeName, fakeTopology, topology)
				ginkgo.By(fmt.Sprintf("add topology %s=%s in node %s", fakeTopology, topology, nodeName))
			}
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Remove fake topology")
			for _, nodeName := range testNodeNames {
				framework.RemoveLabelOffNode(f.ClientSet, nodeName, fakeTopology)
			}

			ls := metav1.SetAsLabelSelector(map[string]string{
				"e2e-test-reservation": "true",
			})
			reservationList, err := f.KoordinatorClientSet.SchedulingV1alpha1().Reservations().List(context.TODO(), metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(ls),
			})
			framework.ExpectNoError(err)
			for _, v := range reservationList.Items {
				err := f.KoordinatorClientSet.SchedulingV1alpha1().Reservations().Delete(context.TODO(), v.Name, metav1.DeleteOptions{})
				framework.ExpectNoError(err)
			}
		})

		ginkgo.It("Schedule Pods with co-scheduling and topology aware constraint", func() {
			ginkgo.By("Trying to create pods")
			numPods := 4
			requests := corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			}
			topologyAwareConstraint := &apiext.TopologyAwareConstraint{
				Name: "test-topology-aware",
				Required: &apiext.TopologyConstraint{
					Topologies: []apiext.TopologyAwareTerm{
						{
							Key: fakeTopology,
						},
					},
				},
			}
			data, err := json.Marshal(topologyAwareConstraint)
			framework.ExpectNoError(err)

			rsConfig := pauseRSConfig{
				Replicas: int32(numPods),
				PodConfig: pausePodConfig{
					Name:      "test-pod",
					Namespace: f.Namespace.Name,
					Labels: map[string]string{
						"success":             "true",
						"test-topology-aware": "true",
					},
					Annotations: map[string]string{
						apiext.AnnotationGangName:                "test-topology-aware",
						apiext.AnnotationGangMinNum:              strconv.Itoa(numPods),
						apiext.AnnotationTopologyAwareConstraint: string(data),
					},
					Resources: &corev1.ResourceRequirements{
						Limits:   requests,
						Requests: requests,
					},
					SchedulerName: "koord-scheduler",
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 100,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"test-topology-aware": "true",
											},
										},
										TopologyKey: corev1.LabelHostname,
									},
								},
							},
						},
					},
				},
			}
			runPauseRS(f, rsConfig)

			podList, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).List(context.TODO(), metav1.ListOptions{})
			framework.ExpectNoError(err)

			scheduledTopologies := map[string]int{}
			for i := range podList.Items {
				pod := &podList.Items[i]
				node, err := f.ClientSet.CoreV1().Nodes().Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
				framework.ExpectNoError(err, fmt.Sprintf("unable to get node %v", pod.Spec.NodeName))
				ginkgo.By(fmt.Sprintf("pod %v scheduled in node %v", pod.Name, node.Name))
				topology := node.Labels[fakeTopology]
				scheduledTopologies[topology]++
			}
			ginkgo.By(fmt.Sprintf("scheduledTopologies %v", scheduledTopologies))
			gomega.Expect(len(scheduledTopologies)).Should(gomega.Equal(1), "unexpected scheduled topologies count")
			for k, v := range scheduledTopologies {
				gomega.Expect(k).Should(gomega.Not(gomega.BeEmpty()), "unexpected scheduled none fake topology nodes")
				gomega.Expect(v).Should(gomega.Equal(4), "unexpected scheduled pods count")
				break
			}
		})

		ginkgo.It("Schedule Reservations with co-scheduling and topology aware constraint", func() {
			ginkgo.By("Trying to create reservations")
			reservation, err := manifest.ReservationFromManifest("scheduling/simple-reservation.yaml")
			framework.ExpectNoError(err, "unable to load reservation")

			topologyAwareConstraint := &apiext.TopologyAwareConstraint{
				Name: "test-topology-aware",
				Required: &apiext.TopologyConstraint{
					Topologies: []apiext.TopologyAwareTerm{
						{
							Key: fakeTopology,
						},
					},
				},
			}
			data, err := json.Marshal(topologyAwareConstraint)
			framework.ExpectNoError(err)
			if reservation.Annotations == nil {
				reservation.Annotations = map[string]string{}
			}
			reservation.Annotations[apiext.AnnotationTopologyAwareConstraint] = string(data)
			reservation.Annotations[apiext.AnnotationGangName] = "test-reservation-topology-aware"
			numReservations := 4
			reservation.Annotations[apiext.AnnotationGangMinNum] = strconv.Itoa(numReservations)

			if reservation.Labels == nil {
				reservation.Labels = map[string]string{}
			}
			reservation.Labels["test-topology-aware"] = "true"

			var allReservations []*schedulingv1alpha1.Reservation
			for i := 0; i < numReservations; i++ {
				reservationCopy := reservation.DeepCopy()
				reservationCopy.Name = fmt.Sprintf("%s-%d", reservationCopy.Name, i)
				reservationCopy.Spec.Template.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"test-topology-aware": "true",
										},
									},
									TopologyKey: corev1.LabelHostname,
								},
							},
						},
					},
				}
				reservationCopy, err := f.KoordinatorClientSet.SchedulingV1alpha1().Reservations().Create(context.TODO(), reservationCopy, metav1.CreateOptions{})
				framework.ExpectNoError(err, "unable to create reservation")
				allReservations = append(allReservations, reservationCopy)
			}

			for i, r := range allReservations {
				allReservations[i] = waitingForReservationScheduled(f.KoordinatorClientSet, r)
			}

			scheduledTopologies := map[string]int{}
			for _, r := range allReservations {
				node, err := f.ClientSet.CoreV1().Nodes().Get(context.TODO(), r.Status.NodeName, metav1.GetOptions{})
				framework.ExpectNoError(err, fmt.Sprintf("unable to get node %v", r.Status.NodeName))
				ginkgo.By(fmt.Sprintf("reservation %v scheduled in node %v", r.Name, node.Name))
				topology := node.Labels[fakeTopology]
				scheduledTopologies[topology]++
			}
			ginkgo.By(fmt.Sprintf("scheduledTopologies %v", scheduledTopologies))
			gomega.Expect(len(scheduledTopologies)).Should(gomega.Equal(1), "unexpected scheduled topologies count")
			for k, v := range scheduledTopologies {
				gomega.Expect(k).Should(gomega.Not(gomega.BeEmpty()), "unexpected scheduled none fake topology nodes")
				gomega.Expect(v).Should(gomega.Equal(4), "unexpected scheduled pods count")
				break
			}
		})
	})
})
