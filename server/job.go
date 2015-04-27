package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"

	lib "github.com/bazooka-ci/bazooka/commons"
	"github.com/bazooka-ci/bazooka/commons/mongo"
	docker "github.com/bywan/go-dockercommand"
)

const (
	buildFolderPattern = "%s/build/%s/%s"     // $bzk_home/build/$projectId/$buildId
	logFolderPattern   = "%s/build/%s/%s/log" // $bzk_home/build/$projectId/$buildId/log
)

func (c *context) startBitbucketJob(params map[string]string, body bodyFunc) (*response, error) {
	var bitbucketPayload BitbucketPayload

	body(&bitbucketPayload)

	if len(bitbucketPayload.Commits) == 0 {
		return badRequest("no commit found in Bitbucket payload")
	}

	//TODO(julienvey) Order by timestamp to find the last commit instead of trusting
	// Bitbucket to give us the commits in the right order

	if len(bitbucketPayload.Commits[0].RawNode) == 0 {
		return badRequest("RawNode is empty in Bitbucket payload")
	}

	return c.startJob(params, lib.StartJob{
		ScmReference: bitbucketPayload.Commits[0].RawNode,
	})

}

func (c *context) startGithubJob(params map[string]string, body bodyFunc) (*response, error) {
	var githubPayload GithubPayload

	body(&githubPayload)

	if len(githubPayload.HeadCommit.ID) == 0 {
		return badRequest("HeadCommit is empty in Github payload")
	}

	return c.startJob(params, lib.StartJob{
		ScmReference: githubPayload.HeadCommit.ID,
	})

}

func (c *context) startStandardJob(params map[string]string, body bodyFunc) (*response, error) {

	var startJob lib.StartJob

	body(&startJob)

	if len(startJob.ScmReference) == 0 {
		return badRequest("reference is mandatory")
	}

	return c.startJob(params, startJob)
}

func (c *context) startJob(params map[string]string, startJob lib.StartJob) (*response, error) {

	project, err := c.Connector.GetProjectById(params["id"])
	if err != nil {
		if err.Error() != "not found" {
			return nil, err
		}
		return notFound("project not found")
	}

	client, err := docker.NewDocker(c.DockerEndpoint)
	if err != nil {
		return nil, err
	}

	orchestrationImage, err := c.Connector.GetImage("orchestration")
	if err != nil {
		return nil, &errorResponse{500, fmt.Sprintf("Failed to retrieve the orchestration image: %v", err)}
	}

	runningJob := &lib.Job{
		ProjectID:  project.ID,
		Started:    time.Now(),
		Parameters: startJob.Parameters,
	}

	if err := c.Connector.AddJob(runningJob); err != nil {
		return nil, err
	}

	for _, v := range startJob.Parameters {
		if !strings.Contains(v, "=") {
			return nil, &errorResponse{400, fmt.Sprintf("Environment variable %v is empty", v)}
		}
	}
	jobParameters, err := json.Marshal(startJob.Parameters)
	if err != nil {
		return nil, err
	}

	buildFolder := fmt.Sprintf(buildFolderPattern, c.Env[BazookaEnvHome], runningJob.ProjectID, runningJob.ID)
	orchestrationEnv := map[string]string{
		"BZK_SCM":            project.ScmType,
		"BZK_SCM_URL":        project.ScmURI,
		"BZK_SCM_REFERENCE":  startJob.ScmReference,
		"BZK_HOME":           buildFolder,
		"BZK_SRC":            buildFolder + "/source",
		"BZK_PROJECT_ID":     project.ID,
		"BZK_JOB_ID":         runningJob.ID,
		"BZK_DOCKERSOCK":     c.Env[BazookaEnvDockerSock],
		"BZK_JOB_PARAMETERS": string(jobParameters),
		BazookaEnvMongoAddr:  c.Env[BazookaEnvMongoAddr],
		BazookaEnvMongoPort:  c.Env[BazookaEnvMongoPort],
	}

	buildFolderLocal := fmt.Sprintf(buildFolderPattern, "/bazooka", runningJob.ProjectID, runningJob.ID)

	projectSSHKey, err := c.Connector.GetProjectKey(project.ID)
	if err != nil {
		_, keyNotFound := err.(*mongo.NotFoundError)
		if !keyNotFound {
			return nil, err
		}
		//Use Global Key if provided
		if len(c.Env[BazookaEnvSCMKeyfile]) > 0 {
			orchestrationEnv["BZK_SCM_KEYFILE"] = c.Env[BazookaEnvSCMKeyfile]
		}
	} else {
		err = os.MkdirAll(buildFolderLocal, 0644)
		if err != nil {
			return nil, err
		}

		err = ioutil.WriteFile(fmt.Sprintf("%s/key", buildFolderLocal), []byte(projectSSHKey.Content), 0600)
		if err != nil {
			return nil, err
		}
		orchestrationEnv["BZK_SCM_KEYFILE"] = fmt.Sprintf("%s/key", buildFolder)
	}

	projectCryptoKey, err := c.Connector.GetProjectCryptoKey(project.ID)

	if err != nil {
		_, keyNotFound := err.(*mongo.NotFoundError)
		if !keyNotFound {
			return nil, err
		}
	} else {
		err = os.MkdirAll(buildFolderLocal, 0644)
		if err != nil {
			return nil, err
		}

		err = ioutil.WriteFile(fmt.Sprintf("%s/crypto-key", buildFolderLocal), []byte(projectCryptoKey.Content), 0600)
		if err != nil {
			return nil, err
		}
		orchestrationEnv["BZK_CRYPTO_KEYFILE"] = fmt.Sprintf("%s/crypto-key", buildFolder)
	}

	orchestrationVolumes := []string{
		fmt.Sprintf("%s:/bazooka", buildFolder),
		fmt.Sprintf("%s:/var/run/docker.sock", c.Env[BazookaEnvDockerSock]),
	}

	reuseScmCheckout := project.Config["bzk.scm.reuse"] == "true"
	if reuseScmCheckout {
		hostSharedSourceFolder := fmt.Sprintf("%s/build/%s/source", c.Env[BazookaEnvHome], runningJob.ProjectID)
		containerSharedSourceFolder := fmt.Sprintf("/bazooka/build/%s/source", runningJob.ProjectID)

		_, err := os.Stat(containerSharedSourceFolder)
		if err != nil {
			if os.IsNotExist(err) {
				err = os.MkdirAll(containerSharedSourceFolder, 0644)
				if err != nil {
					return nil, fmt.Errorf("Failed to create a shared source directory for project %s, job %s: %v",
						runningJob.ProjectID, runningJob.ID, err)
				}
			} else {
				return nil, fmt.Errorf("Failed to stat the shared source directory for project %s, job %s: %v",
					runningJob.ProjectID, runningJob.ID, err)
			}
		}

		orchestrationEnv["BZK_SRC"] = hostSharedSourceFolder
		orchestrationEnv["BZK_REUSE_SCM_CHECKOUT"] = "1"

		orchestrationVolumes = append(orchestrationVolumes, fmt.Sprintf("%s:/bazooka/source", hostSharedSourceFolder))
	}

	cacheDirs := project.Config["bzk.cache.dirs"]
	if len(cacheDirs) > 0 {
		cacheMounts := map[string]string{}
		dirs := strings.Split(cacheDirs, ":")

		for _, containerDir := range dirs {
			hostCachedDir := fmt.Sprintf("%s/build/%s/cache/%s", c.Env[BazookaEnvHome], runningJob.ProjectID, containerDir)
			if err := os.MkdirAll(hostCachedDir, 0644); err != nil {
				return nil, fmt.Errorf("Failed to create cached dir %s: %v", hostCachedDir, err)
			}
			cacheMounts[hostCachedDir] = containerDir
		}

		cacheMountsJson, err := json.Marshal(cacheMounts)
		if err != nil {
			return nil, err
		}
		orchestrationEnv["BZK_CACHE_MOUNTS"] = string(cacheMountsJson)
	}

	container, err := client.Run(&docker.RunOptions{
		Image:       orchestrationImage,
		VolumeBinds: orchestrationVolumes,
		Env:         orchestrationEnv,
		Detach:      true,
	})

	// remove the container at the end of its execution
	go func(container *docker.Container) {
		exitCode, err := container.Wait()
		if err != nil {
			log.Errorf("Error while listening container %s", container.ID, err)
		}

		if exitCode != 0 {
			log.Errorf("Error during execution of Orchestrator container. Check Docker container logs, id is %s\n", container.ID())
			return
		}

		err = container.Remove(&docker.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		if err != nil {
			log.Errorf("Cannot remove container %s", container.ID)
		}
	}(container)

	runningJob.OrchestrationID = container.ID()
	log.WithFields(log.Fields{
		"job_id":           runningJob.ID,
		"project_id":       runningJob.ProjectID,
		"orchestration_id": runningJob.OrchestrationID,
	}).Info("Starting job")

	err = c.Connector.SetJobOrchestrationId(runningJob.ID, container.ID())
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	return accepted(runningJob, "/job/"+runningJob.ID)
}

func (c *context) getJob(params map[string]string, body bodyFunc) (*response, error) {

	job, err := c.Connector.GetJobByID(params["id"])
	if err != nil {
		if err.Error() != "not found" {
			return nil, err
		}
		return notFound("job not found")
	}

	return ok(&job)
}

func (c *context) getJobs(params map[string]string, body bodyFunc) (*response, error) {

	jobs, err := c.Connector.GetJobs(params["id"])
	if err != nil {
		return nil, err
	}

	return ok(&jobs)
}

func (c *context) getAllJobs(params map[string]string, body bodyFunc) (*response, error) {

	jobs, err := c.Connector.GetAllJobs()
	if err != nil {
		return nil, err
	}

	return ok(&jobs)
}

func (c *context) getJobLog(params map[string]string, body bodyFunc) (*response, error) {

	log, err := c.Connector.GetLog(&mongo.LogExample{
		JobID: params["id"],
	})
	if err != nil {
		if err.Error() != "not found" {
			return nil, err
		}
		return notFound("log not found")
	}

	return ok(&log)
}
