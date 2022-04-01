/**
 * Copyright (c) 2018 Dell Inc., or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package e2eutil

import (
	goctx "context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	api "github.com/pravega/zookeeper-operator/api/v1beta1"
)

var (
	RetryInterval        = time.Second * 5
	Timeout              = time.Second * 60
	CleanupRetryInterval = time.Second * 5
	CleanupTimeout       = time.Second * 5
	ReadyTimeout         = time.Minute * 5
	UpgradeTimeout       = time.Minute * 10
	TerminateTimeout     = time.Minute * 5
	VerificationTimeout  = time.Minute * 5
)

// CreateCluster creates a ZookeeperCluster CR with the desired spec
func CreateCluster(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) (*api.ZookeeperCluster, error) {
	t.Logf("creating zookeeper cluster: %s", z.Name)
	z.Spec.Image.PullPolicy = "IfNotPresent"
	err := k8client.Create(goctx.TODO(), z)
	if err != nil {
		return nil, fmt.Errorf("failed to create CR: %v", err)
	}

	zk := &api.ZookeeperCluster{}
	err = k8client.Get(goctx.TODO(), types.NamespacedName{Namespace: z.Namespace, Name: z.Name}, zk)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain created CR: %v", err)
	}
	t.Logf("created zookeeper cluster: %s", zk.Name)
	return z, nil
}

// DeleteCluster deletes the ZookeeperCluster CR specified by cluster spec
func DeleteCluster(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) error {
	t.Logf("deleting zookeeper cluster: %s", z.Name)
	err := k8client.Delete(goctx.TODO(), z)
	if err != nil {
		return fmt.Errorf("failed to delete CR: %v", err)
	}

	t.Logf("deleted zookeeper cluster: %s", z.Name)
	return nil
}

// UpdateCluster updates the ZookeeperCluster CR
func UpdateCluster(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) error {
	t.Logf("updating zookeeper cluster: %s", z.Name)
	err := k8client.Update(goctx.TODO(), z)
	if err != nil {
		return fmt.Errorf("failed to update CR: %v", err)
	}

	t.Logf("updated zookeeper cluster: %s", z.Name)
	return nil
}

// GetCluster returns the latest ZookeeperCluster CR
func GetCluster(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) (*api.ZookeeperCluster, error) {
	zk := &api.ZookeeperCluster{}
	err := k8client.Get(goctx.TODO(), types.NamespacedName{Namespace: z.Namespace, Name: z.Name}, zk)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain created CR: %v", err)
	}
	t.Logf("zk cluster has ready replicas %v", zk.Status.ReadyReplicas)
	return zk, nil
}

// WaitForClusterToBecomeReady will wait until all cluster pods are ready
func WaitForClusterToBecomeReady(t *testing.T, k8client client.Client, z *api.ZookeeperCluster, size int) error {
	t.Logf("waiting for cluster pods to become ready: %s", z.Name)
	err := wait.Poll(RetryInterval, ReadyTimeout, func() (done bool, err error) {
		cluster, err := GetCluster(t, k8client, z)
		if err != nil {
			return false, err
		}

		t.Logf("\twaiting for pods to become ready (%d/%d), pods (%v)", cluster.Status.ReadyReplicas, size, cluster.Status.Members.Ready)

		_, condition := cluster.Status.GetClusterCondition(api.ClusterConditionPodsReady)
		if condition != nil && condition.Status == corev1.ConditionTrue && cluster.Status.ReadyReplicas == int32(size) {
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		return err
	}
	t.Logf("zookeeper cluster ready: %s", z.Name)
	return nil

}

// WaitForClusterToUpgrade will wait until all pods are upgraded
func WaitForClusterToUpgrade(t *testing.T, k8client client.Client, z *api.ZookeeperCluster, targetVersion string) error {
	t.Logf("waiting for cluster to upgrade: %s", z.Name)

	err := wait.Poll(RetryInterval, UpgradeTimeout, func() (done bool, err error) {
		cluster, err := GetCluster(t, k8client, z)
		if err != nil {
			return false, err
		}

		_, upgradeCondition := cluster.Status.GetClusterCondition(api.ClusterConditionUpgrading)
		_, errorCondition := cluster.Status.GetClusterCondition(api.ClusterConditionError)

		t.Logf("\twaiting for cluster to upgrade (upgrading: %s; error: %s)", upgradeCondition.Status, errorCondition.Status)

		if errorCondition.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("failed upgrading cluster: [%s] %s", errorCondition.Reason, errorCondition.Message)
		}

		if upgradeCondition.Status == corev1.ConditionFalse && cluster.Status.CurrentVersion == targetVersion {
			// Cluster upgraded
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		return err
	}

	t.Logf("zookeeper cluster upgraded: %s", z.Name)
	return nil
}

// WaitForClusterToTerminate will wait until all cluster pods are terminated
func WaitForClusterToTerminate(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) error {
	t.Logf("waiting for zookeeper cluster to terminate: %s", z.Name)

	listOptions := []client.ListOption{
		client.InNamespace(z.GetNamespace()),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(map[string]string{"app": z.GetName()})},
	}

	// Wait for Pods to terminate
	err := wait.Poll(RetryInterval, TerminateTimeout, func() (done bool, err error) {
		podList := corev1.PodList{}
		err = k8client.List(goctx.TODO(), &podList, listOptions...)
		if err != nil {
			return false, err
		}

		var names []string
		for i := range podList.Items {
			pod := &podList.Items[i]
			names = append(names, pod.Name)
		}
		t.Logf("waiting for pods to terminate, running pods (%v)", names)
		if len(names) != 0 {
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return err
	}

	// Wait for PVCs to terminate
	err = wait.Poll(RetryInterval, TerminateTimeout, func() (done bool, err error) {
		pvcList := corev1.PersistentVolumeClaimList{}
		err = k8client.List(goctx.TODO(), &pvcList, listOptions...)
		if err != nil {
			return false, err
		}

		var names []string
		for i := range pvcList.Items {
			pvc := &pvcList.Items[i]
			names = append(names, pvc.Name)
		}
		t.Logf("waiting for pvc to terminate (%v)", names)
		if len(names) != 0 {
			return false, nil
		}
		return true, nil

	})

	if err != nil {
		return err
	}

	t.Logf("zookeeper cluster terminated: %s", z.Name)
	return nil
}
func DeletePods(t *testing.T, k8client client.Client, z *api.ZookeeperCluster, size int) error {
	listOptions := []client.ListOption{
		client.InNamespace(z.GetNamespace()),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(map[string]string{"app": z.GetName()})},
	}
	podList := corev1.PodList{}
	err := k8client.List(goctx.TODO(), &podList, listOptions...)
	if err != nil {
		return err
	}
	pod := &corev1.Pod{}

	for i := 0; i < size; i++ {
		pod = &podList.Items[i]
		t.Logf("podnameis %v", pod.Name)
		err := k8client.Delete(goctx.TODO(), pod)
		if err != nil {
			return fmt.Errorf("failed to delete pod: %v", err)
		}

		t.Logf("deleted zookeeper pod: %s", pod.Name)

	}
	return nil
}
func GetPods(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) (*corev1.PodList, error) {
	listOptions := []client.ListOption{
		client.InNamespace(z.GetNamespace()),
		client.MatchingLabels(map[string]string{"app": z.GetName()}),
	}
	podList := corev1.PodList{}
	err := k8client.List(goctx.TODO(), &podList, listOptions...)
	return &podList, err
}
func CheckAdminService(t *testing.T, k8client client.Client, z *api.ZookeeperCluster) error {
	serviceList := corev1.ServiceList{}
	listOptions := []client.ListOption{client.InNamespace(z.GetNamespace()),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(map[string]string{"app": z.GetName()})}}
	err := k8client.List(goctx.TODO(), &serviceList, listOptions...)
	if err != nil {
		return err
	}

	for _, sn := range serviceList.Items {
		if sn.Name == "zookeeper-admin-server" {
			t.Logf("Admin service is enabled")
			t.Logf("servicenameis %v", sn.Name)
			return nil
		}
	}
	return fmt.Errorf("Admin Service is not enabled")
}
