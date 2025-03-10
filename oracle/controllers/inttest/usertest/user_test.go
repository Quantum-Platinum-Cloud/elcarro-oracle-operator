// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package usertest

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	// Enable GCP auth for k8s client
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	commonv1alpha1 "github.com/GoogleCloudPlatform/elcarro-oracle-operator/common/api/v1alpha1"
	"github.com/GoogleCloudPlatform/elcarro-oracle-operator/oracle/api/v1alpha1"
	"github.com/GoogleCloudPlatform/elcarro-oracle-operator/oracle/controllers/testhelpers"
	"github.com/GoogleCloudPlatform/elcarro-oracle-operator/oracle/pkg/k8s"
)

func TestUser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User operations")
}

var (
	// Global variable, to be accessible by AfterSuite
	k8sEnv        = testhelpers.K8sOperatorEnvironment{}
	instanceName  = "mydb"
	databaseName  = "pdb1"
	pod           = "mydb-sts-0"
	projectId     = os.Getenv("PROW_PROJECT")
	targetCluster = os.Getenv("PROW_CLUSTER")
	targetZone    = os.Getenv("PROW_CLUSTER_ZONE")
	userPwdBefore = map[string]string{
		"GPDB_ADMIN": "google",
		"superuser":  "superpassword",
		"scott":      "tiger",
		"proberuser": "proberpassword",
	}
	userPwdAfter = map[string]string{
		"GPDB_ADMIN": "google1",
		"superuser":  "superpassword1",
		"scott":      "tiger1",
		"proberuser": "proberpassword1",
	}
	log = logf.FromContext(nil)
)

// Initial setup before test suite.
var _ = BeforeSuite(func() {
	klog.SetOutput(GinkgoWriter)
	logf.SetLogger(klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)))

	log = logf.FromContext(nil)
	// Note that these GSM + WI setup steps are re-runnable.
	// If the env fulfills, no error should occur.

	// Check if project info is initialized
	Expect(projectId).ToNot(BeEmpty())
	Expect(targetCluster).ToNot(BeEmpty())
	Expect(targetZone).NotTo(BeEmpty())
	testhelpers.EnableGsmApi()
	testhelpers.EnableIamApi()
	prepareTestUsersAndGrantAccess()
	testhelpers.EnableWiWithNodePool()
})

// In case of Ctrl-C clean up the last valid k8sEnv.
var _ = AfterSuite(func() {
	k8sEnv.Close()
})

var _ = Describe("User operations", func() {
	BeforeEach(func() {
		defer GinkgoRecover()
		initEnvBeforeEachTest()
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			testhelpers.PrintSimpleDebugInfo(k8sEnv, instanceName, "GCLOUD")
		}
		k8sEnv.Close()
	})

	testUpdateUser := func(version string, edition string) {
		It("Should test users creation with GSM", func() {
			testhelpers.CreateSimpleInstance(k8sEnv, instanceName, version, edition)

			// Wait until DatabaseInstanceReady = True
			instKey := client.ObjectKey{Namespace: k8sEnv.CPNamespace, Name: instanceName}
			testhelpers.WaitForInstanceConditionState(k8sEnv, instKey, k8s.DatabaseInstanceReady, metav1.ConditionTrue, k8s.CreateComplete, 20*time.Minute)

			// Create PDB
			testhelpers.CreateSimplePdbWithDbObj(k8sEnv, &v1alpha1.Database{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: k8sEnv.CPNamespace,
					Name:      databaseName,
				},
				Spec: v1alpha1.DatabaseSpec{
					DatabaseSpec: commonv1alpha1.DatabaseSpec{
						Name:     databaseName,
						Instance: instanceName,
					},
					AdminPasswordGsmSecretRef: &commonv1alpha1.GsmSecretReference{
						ProjectId: projectId,
						SecretId:  "GPDB_ADMIN",
						Version:   "1",
					},
					Users: []v1alpha1.UserSpec{
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "superuser",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "superuser",
										Version:   "1",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "scott",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "scott",
										Version:   "1",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "proberuser",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "proberuser",
										Version:   "1",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
					},
				},
			})
			createdDatabase := &v1alpha1.Database{}
			objKey := client.ObjectKey{Namespace: k8sEnv.CPNamespace, Name: databaseName}
			err := k8sEnv.K8sClient.Get(k8sEnv.Ctx, client.ObjectKey{Namespace: k8sEnv.CPNamespace, Name: databaseName}, createdDatabase)
			Expect(err).Should(Succeed())

			// Note the we might not need a separate test for user creation
			// as BeforeEach function has covered this scenario already.
			By("Verify PDB user connectivity with initial passwords")
			waitForUserPasswordSyncVersion(createdDatabase, "1")
			testhelpers.K8sVerifyUserConnectivity(pod, k8sEnv.CPNamespace, databaseName, userPwdBefore)

			By("DB is ready, updating user secret version")

			testhelpers.K8sUpdateWithRetry(k8sEnv.K8sClient, k8sEnv.Ctx,
				objKey,
				createdDatabase,
				func(obj *client.Object) {
					databaseToUpdate := (*obj).(*v1alpha1.Database)
					databaseToUpdate.Spec.AdminPasswordGsmSecretRef = &commonv1alpha1.GsmSecretReference{
						ProjectId: projectId,
						SecretId:  "GPDB_ADMIN",
						Version:   "2",
					}
					databaseToUpdate.Spec.Users = []v1alpha1.UserSpec{
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "superuser",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "superuser",
										Version:   "2",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "scott",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "scott",
										Version:   "2",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
						{
							UserSpec: commonv1alpha1.UserSpec{
								Name: "proberuser",
								CredentialSpec: commonv1alpha1.CredentialSpec{
									GsmSecretRef: &commonv1alpha1.GsmSecretReference{
										ProjectId: projectId,
										SecretId:  "proberuser",
										Version:   "2",
									},
								},
							},
							Privileges: []v1alpha1.PrivilegeSpec{"connect"},
						},
					}
				})

			// Verify if both PDB ready and user ready status are expected.
			waitForUserPasswordSyncVersion(createdDatabase, "2")

			// Resolve password sync latency between Config Server and Oracle DB.
			// Even after we checked PDB status is ready and user sync complete.
			time.Sleep(5 * time.Second)

			By("Verify PDB user connectivity with new passwords")
			testhelpers.K8sVerifyUserConnectivity(pod, k8sEnv.CPNamespace, databaseName, userPwdAfter)
		})
	}

	Context("Oracle 19.3 EE", func() {
		testUpdateUser("19.3", "EE")
	})
	Context("Oracle 18c XE", func() {
		testUpdateUser("18c", "XE")
	})
})

func waitForUserPasswordSyncVersion(createdDatabase *v1alpha1.Database, version string) {
	// Verify if both PDB ready and user ready status are expected.
	Eventually(func() bool {
		Expect(k8sEnv.K8sClient.Get(k8sEnv.Ctx, client.ObjectKey{Namespace: k8sEnv.CPNamespace, Name: databaseName}, createdDatabase)).Should(Succeed())
		cond := k8s.FindCondition(createdDatabase.Status.Conditions, k8s.UserReady)
		if !k8s.ConditionReasonEquals(cond, k8s.SyncComplete) || !k8s.ConditionStatusEquals(cond, metav1.ConditionTrue) {
			log.Info("Waiting "+k8s.UserReady, "reason", cond.Reason, "status", cond.Status)
			return false
		}
		for _, v := range createdDatabase.Status.UserResourceVersions {
			parts := strings.Split(v, "/")
			curVersion := parts[len(parts)-1]
			if curVersion != version {
				log.Info("Waiting "+k8s.UserReady, "version", curVersion, "expecting", version)
				return false
			}
		}
		return true
	}, 2*time.Minute, 5*time.Second).Should(Equal(true))
}

func prepareTestUsersAndGrantAccess() {
	// Prepare test users and grant GMS permission
	for k, v := range userPwdBefore {
		checkUser := exec.Command("gcloud", "secrets", "describe", k)
		if checkUserOutput, err := checkUser.CombinedOutput(); err != nil {
			log.Info("gcloud secrets describe "+k, "output", string(checkUserOutput))

			// Prepare two password files for initiating GSM secret
			f1, err := ioutil.TempFile("", "")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(f1.Name())
			err = ioutil.WriteFile(f1.Name(), []byte(v), os.FileMode(0600))
			Expect(err).NotTo(HaveOccurred())

			f2, err := ioutil.TempFile("", "")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(f2.Name())
			newPassword, ok := userPwdAfter[k]
			Expect(ok).Should(Equal(true))
			err = ioutil.WriteFile(f2.Name(), []byte(newPassword), os.FileMode(0600))
			Expect(err).NotTo(HaveOccurred())

			// Create user with credential file f1.
			cmd := exec.Command("gcloud", "secrets", "create", k, "--replication-policy=automatic", "--data-file="+f1.Name())
			out, err := cmd.CombinedOutput()
			// Omitted password.
			log.Info("gcloud secrets create "+k, "output", string(out))
			Expect(err).NotTo(HaveOccurred())

			// Add user secret with credential file f2.
			cmd = exec.Command("gcloud", "secrets", "versions", "add", k, "--data-file="+f2.Name())
			out, err = cmd.CombinedOutput()
			// Omitted password.
			log.Info("gcloud secrets add "+k+" v2", "output", string(out))
			Expect(err).NotTo(HaveOccurred())
		}

		// Grant GSM secret access role to the our test service account.
		Expect(retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
			cmd := exec.Command("gcloud",
				"secrets", "add-iam-policy-binding", k, "--role=roles/secretmanager.secretAccessor",
				"--member="+"serviceAccount:"+testhelpers.GCloudServiceAccount())
			out, err := cmd.CombinedOutput()
			log.Info("gcloud secrets service-accounts add-iam-policy-binding", "output", string(out))
			return err
		})).To(Succeed())
	}
}

func initEnvBeforeEachTest() {
	namespace := testhelpers.RandName("user-test")
	k8sEnv.Init(namespace, namespace)
	// Allow the k8s [namespace/default] service account access to GCS buckets
	testhelpers.SetupServiceAccountBindingBetweenGcpAndK8s(k8sEnv)
}
