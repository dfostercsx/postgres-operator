/*
 Copyright 2017 Crunchy Data Solutions, Inc.
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

// Package cluster holds the cluster TPR logic and definitions
// A cluster is comprised of a master service, replica service,
// master deployment, and replica deployment
package cluster

import (
	"bytes"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/crunchydata/postgres-operator/operator/pvc"
	"github.com/crunchydata/postgres-operator/operator/util"
	"github.com/crunchydata/postgres-operator/tpr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	v1batch "k8s.io/client-go/pkg/apis/batch/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/rest"
	"text/template"
	"time"
)

var JobTemplate1 *template.Template

type JobTemplateFields struct {
	Name              string
	OLD_PVC_NAME      string
	NEW_PVC_NAME      string
	CCP_IMAGE_TAG     string
	OLD_DATABASE_NAME string
	NEW_DATABASE_NAME string
	OLD_VERSION       string
	NEW_VERSION       string
}

const DB_UPGRADE_JOB_PATH = "/pgconf/postgres-operator/database/1/database-upgrade-job.json"

func init() {

	JobTemplate1 = util.LoadTemplate(DB_UPGRADE_JOB_PATH)
}

func (r ClusterStrategy1) MinorUpgrade(clientset *kubernetes.Clientset, tprclient *rest.RESTClient, cl *tpr.PgCluster, upgrade *tpr.PgUpgrade, namespace string) error {
	var err error
	var replicaDoc, masterDoc bytes.Buffer
	var replicaDeploymentResult, deploymentResult *v1beta1.Deployment
	var replicaName = cl.Spec.Name + REPLICA_SUFFIX

	log.Info("minor cluster upgrade using Strategy 1 in namespace " + namespace)

	err = shutdownCluster(clientset, tprclient, cl, namespace)
	if err != nil {
		log.Error("error in shutdownCluster " + err.Error())
	}

	//create the master deployment

	deploymentFields := DeploymentTemplateFields{
		Name:                 cl.Spec.Name,
		ClusterName:          cl.Spec.Name,
		Port:                 cl.Spec.Port,
		CCP_IMAGE_TAG:        upgrade.Spec.CCP_IMAGE_TAG,
		PVC_NAME:             cl.Spec.PVC_NAME,
		PG_MASTER_USER:       cl.Spec.PG_MASTER_USER,
		PG_MASTER_PASSWORD:   cl.Spec.PG_MASTER_PASSWORD,
		PGDATA_PATH_OVERRIDE: cl.Spec.Name,
		PG_USER:              cl.Spec.PG_USER,
		PG_PASSWORD:          cl.Spec.PG_PASSWORD,
		PG_DATABASE:          cl.Spec.PG_DATABASE,
		PG_ROOT_PASSWORD:     cl.Spec.PG_ROOT_PASSWORD,
		SECURITY_CONTEXT:     util.CreateSecContext(cl.Spec.FS_GROUP, cl.Spec.SUPPLEMENTAL_GROUPS),
	}

	err = DeploymentTemplate1.Execute(&masterDoc, deploymentFields)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	deploymentDocString := masterDoc.String()
	log.Info(deploymentDocString)

	deployment := v1beta1.Deployment{}
	err = json.Unmarshal(masterDoc.Bytes(), &deployment)
	if err != nil {
		log.Error("error unmarshalling master json into Deployment " + err.Error())
		return err
	}

	deploymentResult, err = clientset.Deployments(namespace).Create(&deployment)
	if err != nil {
		log.Error("error creating master Deployment " + err.Error())
		return err
	}
	log.Info("created master Deployment " + deploymentResult.Name + " in namespace " + namespace)

	//create the replica deployment
	replicaDeploymentFields := DeploymentTemplateFields{
		Name:               replicaName,
		ClusterName:        cl.Spec.Name,
		Port:               cl.Spec.Port,
		CCP_IMAGE_TAG:      upgrade.Spec.CCP_IMAGE_TAG,
		PVC_NAME:           cl.Spec.PVC_NAME,
		PG_MASTER_HOST:     cl.Spec.PG_MASTER_HOST,
		PG_MASTER_USER:     cl.Spec.PG_MASTER_USER,
		PG_MASTER_PASSWORD: cl.Spec.PG_MASTER_PASSWORD,
		PG_USER:            cl.Spec.PG_USER,
		PG_PASSWORD:        cl.Spec.PG_PASSWORD,
		PG_DATABASE:        cl.Spec.PG_DATABASE,
		PG_ROOT_PASSWORD:   cl.Spec.PG_ROOT_PASSWORD,
		REPLICAS:           cl.Spec.REPLICAS,
		SECURITY_CONTEXT:   util.CreateSecContext(cl.Spec.FS_GROUP, cl.Spec.SUPPLEMENTAL_GROUPS),
	}

	err = ReplicaDeploymentTemplate1.Execute(&replicaDoc, replicaDeploymentFields)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	replicaDeploymentDocString := replicaDoc.String()
	log.Info(replicaDeploymentDocString)

	replicaDeployment := v1beta1.Deployment{}
	err = json.Unmarshal(replicaDoc.Bytes(), &replicaDeployment)
	if err != nil {
		log.Error("error unmarshalling replica json into Deployment " + err.Error())
		return err
	}

	replicaDeploymentResult, err = clientset.Deployments(namespace).Create(&replicaDeployment)
	if err != nil {
		log.Error("error creating replica Deployment " + err.Error())
		return err
	}
	log.Info("created replica Deployment " + replicaDeploymentResult.Name)

	//update the upgrade TPR status to completed
	err = util.Patch(tprclient, "/spec/upgradestatus", tpr.UPGRADE_COMPLETED_STATUS, "pgupgrades", upgrade.Spec.Name, namespace)
	if err != nil {
		log.Error(err.Error())
	}

	return err

}

func (r ClusterStrategy1) MajorUpgrade(clientset *kubernetes.Clientset, tprclient *rest.RESTClient, cl *tpr.PgCluster, upgrade *tpr.PgUpgrade, namespace string) error {
	var err error

	log.Info("major cluster upgrade using Strategy 1 in namespace " + namespace)
	err = shutdownCluster(clientset, tprclient, cl, namespace)
	if err != nil {
		log.Error("error in shutdownCluster " + err.Error())
	}

	//create the PVC if necessary
	if upgrade.Spec.NEW_PVC_NAME != upgrade.Spec.OLD_PVC_NAME {
		if pvc.Exists(clientset, upgrade.Spec.NEW_PVC_NAME, namespace) {
			log.Info("pvc " + upgrade.Spec.NEW_PVC_NAME + " already exists, will not create")
		} else {
			log.Info("creating pvc " + upgrade.Spec.NEW_PVC_NAME)
			err = pvc.Create(clientset, upgrade.Spec.NEW_PVC_NAME, upgrade.Spec.PVC_ACCESS_MODE, upgrade.Spec.PVC_SIZE, namespace)
			if err != nil {
				log.Error(err.Error())
				return err
			}
			log.Info("created PVC =" + upgrade.Spec.NEW_PVC_NAME + " in namespace " + namespace)
		}
	}

	//upgrade the master data
	jobFields := JobTemplateFields{
		Name:              upgrade.Spec.Name,
		NEW_PVC_NAME:      upgrade.Spec.NEW_PVC_NAME,
		OLD_PVC_NAME:      upgrade.Spec.OLD_PVC_NAME,
		CCP_IMAGE_TAG:     upgrade.Spec.CCP_IMAGE_TAG,
		OLD_DATABASE_NAME: upgrade.Spec.OLD_DATABASE_NAME,
		NEW_DATABASE_NAME: upgrade.Spec.NEW_DATABASE_NAME,
		OLD_VERSION:       upgrade.Spec.OLD_VERSION,
		NEW_VERSION:       upgrade.Spec.NEW_VERSION,
	}

	var doc bytes.Buffer
	err = JobTemplate1.Execute(&doc, jobFields)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	jobDocString := doc.String()
	log.Debug(jobDocString)

	newjob := v1batch.Job{}
	err = json.Unmarshal(doc.Bytes(), &newjob)
	if err != nil {
		log.Error("error unmarshalling json into Job " + err.Error())
		return err
	}

	resultJob, err := clientset.Batch().Jobs(namespace).Create(&newjob)
	if err != nil {
		log.Error("error creating Job " + err.Error())
		return err
	}
	log.Info("created Job " + resultJob.Name)

	//the remainder of the major upgrade is done via the upgrade watcher

	return err

}

func (r ClusterStrategy1) MajorUpgradeFinalize(clientset *kubernetes.Clientset, client *rest.RESTClient, cl *tpr.PgCluster, upgrade *tpr.PgUpgrade, namespace string) error {
	var err error
	var masterDoc, replicaDoc bytes.Buffer
	var replicaDeploymentResult, deploymentResult *v1beta1.Deployment

	log.Info("major cluster upgrade finalize using Strategy 1 in namespace " + namespace)

	//start the master deployment
	deploymentFields := DeploymentTemplateFields{
		Name:                 cl.Spec.Name,
		ClusterName:          cl.Spec.Name,
		Port:                 cl.Spec.Port,
		CCP_IMAGE_TAG:        upgrade.Spec.CCP_IMAGE_TAG,
		PVC_NAME:             upgrade.Spec.NEW_PVC_NAME,
		PG_MASTER_USER:       cl.Spec.PG_MASTER_USER,
		PG_MASTER_PASSWORD:   cl.Spec.PG_MASTER_PASSWORD,
		PGDATA_PATH_OVERRIDE: upgrade.Spec.NEW_DATABASE_NAME,
		PG_USER:              cl.Spec.PG_USER,
		PG_PASSWORD:          cl.Spec.PG_PASSWORD,
		PG_DATABASE:          cl.Spec.PG_DATABASE,
		PG_ROOT_PASSWORD:     cl.Spec.PG_ROOT_PASSWORD,
		SECURITY_CONTEXT:     util.CreateSecContext(cl.Spec.FS_GROUP, cl.Spec.SUPPLEMENTAL_GROUPS),
	}

	err = DeploymentTemplate1.Execute(&masterDoc, deploymentFields)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	deploymentDocString := masterDoc.String()
	log.Info(deploymentDocString)

	deployment := v1beta1.Deployment{}
	err = json.Unmarshal(masterDoc.Bytes(), &deployment)
	if err != nil {
		log.Error("error unmarshalling master json into Deployment " + err.Error())
		return err
	}

	deploymentResult, err = clientset.Deployments(namespace).Create(&deployment)
	if err != nil {
		log.Error("error creating master Deployment " + err.Error())
		return err
	}
	log.Info("created master Deployment " + deploymentResult.Name + " in namespace " + namespace)

	//start the replica deployment

	replicaDeploymentFields := DeploymentTemplateFields{
		Name:               cl.Spec.Name + REPLICA_SUFFIX,
		ClusterName:        cl.Spec.Name,
		Port:               cl.Spec.Port,
		CCP_IMAGE_TAG:      upgrade.Spec.CCP_IMAGE_TAG,
		PVC_NAME:           cl.Spec.PVC_NAME,
		PG_MASTER_HOST:     cl.Spec.PG_MASTER_HOST,
		PG_MASTER_USER:     cl.Spec.PG_MASTER_USER,
		PG_MASTER_PASSWORD: cl.Spec.PG_MASTER_PASSWORD,
		PG_USER:            cl.Spec.PG_USER,
		PG_PASSWORD:        cl.Spec.PG_PASSWORD,
		PG_DATABASE:        cl.Spec.PG_DATABASE,
		PG_ROOT_PASSWORD:   cl.Spec.PG_ROOT_PASSWORD,
		REPLICAS:           cl.Spec.REPLICAS,
		SECURITY_CONTEXT:   util.CreateSecContext(cl.Spec.FS_GROUP, cl.Spec.SUPPLEMENTAL_GROUPS),
	}

	err = ReplicaDeploymentTemplate1.Execute(&replicaDoc, replicaDeploymentFields)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	replicaDeploymentDocString := replicaDoc.String()
	log.Info(replicaDeploymentDocString)

	replicaDeployment := v1beta1.Deployment{}
	err = json.Unmarshal(replicaDoc.Bytes(), &replicaDeployment)
	if err != nil {
		log.Error("error unmarshalling replica json into Deployment " + err.Error())
		return err
	}
	replicaDeploymentResult, err = clientset.Deployments(namespace).Create(&replicaDeployment)
	if err != nil {
		log.Error("error creating replica Deployment " + err.Error())
		return err
	}
	log.Info("created replica Deployment " + replicaDeploymentResult.Name)

	return err

}

//used by both major and minor upgrades
func shutdownCluster(clientset *kubernetes.Clientset, client *rest.RESTClient, cl *tpr.PgCluster, namespace string) error {
	var err error

	var replicaName = cl.Spec.Name + REPLICA_SUFFIX

	//delete the replica deployment
	err = clientset.Deployments(namespace).Delete(replicaName,
		&v1.DeleteOptions{})
	if err != nil {
		log.Error("error deleting replica Deployment " + err.Error())
	}

	log.Info("deleted replica Deployment " + replicaName + " in namespace " + namespace)

	//wait for the replica deployment to delete
	err = util.WaitUntilDeploymentIsDeleted(clientset, replicaName, time.Minute, namespace)
	if err != nil {
		log.Error("error waiting for replica Deployment deletion " + err.Error())
	}

	//delete the master deployment
	err = clientset.Deployments(namespace).Delete(cl.Spec.Name,
		&v1.DeleteOptions{})
	if err != nil {
		log.Error("error deleting master Deployment " + err.Error())
	}

	//wait for the master deployment to delete
	err = util.WaitUntilDeploymentIsDeleted(clientset, cl.Spec.Name, time.Minute, namespace)
	if err != nil {
		log.Error("error waiting for master Deployment deletion " + err.Error())
	}

	//delete replica sets if they exist
	options := v1.ListOptions{}
	options.LabelSelector = "pg-cluster=" + cl.Spec.Name

	var reps *v1beta1.ReplicaSetList
	reps, err = clientset.ReplicaSets(namespace).List(options)
	if err != nil {
		log.Error("error getting cluster replicaset name" + err.Error())
	} else {
		if len(reps.Items) > 0 {
			err = clientset.ReplicaSets(namespace).Delete(reps.Items[0].Name,
				&v1.DeleteOptions{})
			if err != nil {
				log.Error("error deleting cluster replicaset " + err.Error())
			}

			log.Info("deleted cluster replicaset " + reps.Items[0].Name + " in namespace " + namespace)
		}
	}

	return err

}
