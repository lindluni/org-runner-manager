package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-github/v41/github"
	"github.com/sethvargo/go-githubactions"
	"golang.org/x/oauth2"
)

type manager struct {
	ctx    context.Context
	client *github.Client

	actor         string
	body          string
	issueNumber   int
	org           string
	repo          string
	runnerGroup   string
	runnerGroupID int64
	team          string
}

func main() {
	token := githubactions.GetInput("token")
	action := githubactions.GetInput("action")
	org := githubactions.GetInput("org")
	actor := githubactions.GetInput("actor")
	team := githubactions.GetInput("team")
	body := githubactions.GetInput("body")
	body = strings.Replace(body, "\r", "", -1)
	repo := githubactions.GetInput("repo")
	issueNumber, err := strconv.Atoi(githubactions.GetInput("issue_number"))
	if err != nil {
		githubactions.Fatalf("Failed to parse issue number: %v", err)
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	manager := &manager{
		ctx:    ctx,
		client: client,

		actor:       actor,
		body:        body,
		issueNumber: issueNumber,
		org:         org,
		repo:        repo,
		runnerGroup: fmt.Sprintf("ghm-%s", team),
		team:        team,
	}

	if action != "group-create" {
		var found bool
		manager.runnerGroupID, found = manager.retrieveRunnerGroupID()
		if !found {
			githubactions.Fatalf("Failed to retrieve runner group ID")
		}
	}

	if !manager.verifyTeamExists() {
		githubactions.Fatalf("Unable to verify team %s exists", team)
	}

	if !manager.verifyMaintainership() {
		githubactions.Fatalf("Unable to verify you are a maintainer of this team")
	}

	githubactions.Infof("Executing action %s for %s/%s", action, manager.org, actor)
	switch action {
	case "group-create":
		githubactions.Infof("Creating runner group %s", manager.runnerGroup)
		manager.createGroup()
	case "group-delete":
		githubactions.Infof("Deleting runner group %s", manager.runnerGroup)
		manager.deleteGroup()
	case "group-list":
		githubactions.Infof("Listing runner group contents for %s", manager.runnerGroup)
		repos, runners := manager.groupList()
		spew.Dump(repos, runners)
	case "repos-add":
		githubactions.Infof("Adding repos to runner group %s", manager.runnerGroup)
		manager.addRepos()
	case "repos-remove":
		githubactions.Infof("Removing repos from runner group %s", manager.runnerGroup)
		manager.removeRepos()
	case "repos-set":
		githubactions.Infof("Setting repos in runner group %s", manager.runnerGroup)
		manager.setRepos()
	case "token-register":
		githubactions.Infof("Creating registration token")
		token := manager.createRegistrationToken()
		spew.Dump(token)
	case "token-remove":
		githubactions.Infof("Creating removal token")
		token := manager.createRemovalToken()
		spew.Dump(token)
	}
}

func (m *manager) createGroup() {
	group, _, err := m.client.Actions.CreateOrganizationRunnerGroup(m.ctx, m.org, github.CreateRunnerGroupRequest{
		AllowsPublicRepositories: github.Bool(false),
		Name:                     &m.runnerGroup,
		Visibility:               github.String("selected"),
	})
	if err != nil {
		githubactions.Fatalf("Unable to create group: %v", err)
	}
	githubactions.Infof("Created group %s", group.GetName())
}

func (m *manager) deleteGroup() {
	_, err := m.client.Actions.DeleteOrganizationRunnerGroup(m.ctx, m.org, m.runnerGroupID)
	if err != nil {
		githubactions.Fatalf("Unable to delete group: %v", err)
	}
	githubactions.Infof("Deleted group %s", m.runnerGroup)
}

func (m *manager) groupList() ([]string, []string) {
	repos := m.retrieveRunnerGroupRepos()
	runners := m.retrieveRunnerGroupRunners()
	return repos, runners
}

func (m *manager) addRepos() {
	repos := make(map[string]int64)
	for _, repo := range parseRepos(m.body) {
		id := m.retrieveRepoID(repo)
		repos[repo] = id
	}

	for name, id := range repos {
		githubactions.Infof("Adding repo %s to group %s", name, m.runnerGroup)
		_, err := m.client.Actions.AddRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, id)
		if err != nil {
			githubactions.Fatalf("Unable to add repo %d to group %s: %v", name, m.runnerGroup, err)
		}
	}
}

func (m *manager) removeRepos() {
	repos := make(map[string]int64)
	for _, repo := range parseRepos(m.body) {
		id := m.retrieveRepoID(repo)
		repos[repo] = id
	}

	for name, id := range repos {
		githubactions.Infof("Removing repo %s from group %s", name, m.runnerGroup)
		_, err := m.client.Actions.RemoveRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, id)
		if err != nil {
			githubactions.Fatalf("Unable to remove repo %s to group %s: %v", name, m.runnerGroup, err)
		}
	}
}

func (m *manager) setRepos() {
	repos := parseRepos(m.body)
	var repoIDs []int64
	for _, repo := range repos {
		id := m.retrieveRepoID(repo)
		repoIDs = append(repoIDs, id)
	}

	githubactions.Infof("Replacing existing repos for group %s with new repo set: [%s]", m.runnerGroup, strings.Join(repos[:], ", "))
	_, err := m.client.Actions.SetRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, github.SetRepoAccessRunnerGroupRequest{SelectedRepositoryIDs: repoIDs})
	if err != nil {
		githubactions.Fatalf("Unable to replace repos for group %s: %v", m.runnerGroup, err)
	}
}

func (m *manager) createRegistrationToken() *github.RegistrationToken {
	ctx := context.Background()
	token, _, err := m.client.Actions.CreateOrganizationRegistrationToken(ctx, m.org)
	if err != nil {
		githubactions.Fatalf("Unable to create registration token: %v", err)
	}
	githubactions.Infof("Created registration token")
	return token
}

func (m *manager) createRemovalToken() *github.RemoveToken {
	ctx := context.Background()
	token, _, err := m.client.Actions.CreateOrganizationRemoveToken(ctx, m.org)
	if err != nil {
		githubactions.Fatalf("Unable to create removal token: %v", err)
	}
	githubactions.Infof("Created removal token")
	return token
}

func (m *manager) verifyMaintainership() bool {
	ctx := context.Background()
	githubactions.Infof("Verifying %s is a maintainer of the %s/%s team", m.actor, m.org, m.team)
	membership, resp, err := m.client.Teams.GetTeamMembershipBySlug(ctx, m.org, m.team, m.actor)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("%s is not a member of the %s team", m.actor, m.team)
			return false
		}
		githubactions.Errorf("Unable to get team membership for %s: %v", m.actor, err)
		return false
	}
	if membership.GetRole() != "maintainer" {
		githubactions.Errorf("%s is not a maintainer of the %s team", m.actor, m.team)
		return false
	}
	return true
}

func (m *manager) verifyTeamExists() bool {
	ctx := context.Background()
	githubactions.Infof("Verifying team %s/%s exists", m.org, m.team)
	_, resp, err := m.client.Teams.GetTeamBySlug(ctx, m.org, m.team)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("%s/%s does not exist", m.org, m.team)
			return false
		}
		githubactions.Errorf("Unable to get team: %v", err)
		return false
	}
	githubactions.Infof("Team %s/%s exists", m.org, m.team)
	return true
}

func (m *manager) retrieveRunnerGroupID() (int64, bool) {
	githubactions.Infof("Searching for group ID for runner group %s", m.runnerGroup)
	opts := &github.ListOptions{
		PerPage: 100,
	}
	for {
		groups, resp, err := m.client.Actions.ListOrganizationRunnerGroups(context.Background(), m.org, opts)
		if err != nil {
			githubactions.Fatalf("Unable to retrieve runner groups: %v", err)
		}
		for _, group := range groups.RunnerGroups {
			if group.GetName() == m.runnerGroup {
				githubactions.Infof("Found group %s", group.GetName())
				return group.GetID(), true
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return 0, false
}

func parseRepos(body string) []string {
	r := regexp.MustCompile("Repos:.+")
	match := r.FindStringSubmatch(body)[0]
	trimmedMatch := strings.TrimPrefix(match, "Repos:")
	trimmedRepos := strings.Trim(trimmedMatch, "\t \r \n")
	repoList := strings.Split(trimmedRepos, "\\n\\n")
	if len(repoList) != 2 {
		githubactions.Fatalf("Unable to parse repo list, must be in form: repo1,repo2,repo3")
	}
	repos := strings.Split(repoList[1], ",")
	names := trimRepoNames(repos)
	return names
}

func trimRepoNames(repos []string) []string {
	var trimmedRepos []string
	for _, repo := range repos {
		repo = strings.Trim(repo, "\t \r \n")
		trimmedRepos = append(trimmedRepos, repo)
	}
	return trimmedRepos
}

func (m *manager) retrieveRepoID(repoName string) int64 {
	ctx := context.Background()
	githubactions.Infof("Verifying repo %s exists", repoName)
	repo, resp, err := m.client.Repositories.Get(ctx, m.org, repoName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Fatalf("Repo %s does not exist", repoName)
		}
		githubactions.Fatalf("Unable to get repository: %v", err)
	}
	return repo.GetID()
}

func (m *manager) retrieveRunnerGroupRunners() []string {
	githubactions.Infof("Retrieving runners for group %s", m.runnerGroup)
	opts := &github.ListOptions{
		PerPage: 100,
	}
	var groupRunners []string
	for {
		runners, resp, err := m.client.Actions.ListRunnerGroupRunners(m.ctx, m.org, m.runnerGroupID, opts)
		if err != nil {
			githubactions.Fatalf("Unable to retrieve runners: %v", err)
		}
		for _, runner := range runners.Runners {
			groupRunners = append(groupRunners, runner.GetName())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return groupRunners
}

func (m *manager) retrieveRunnerGroupRepos() []string {
	githubactions.Infof("Retrieving repos for runner group %s", m.runnerGroup)
	opts := &github.ListOptions{
		PerPage: 100,
	}
	var groupRepos []string
	for {
		repos, resp, err := m.client.Actions.ListRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, opts)
		if err != nil {
			githubactions.Fatalf("Unable to retrieve repos: %v", err)
		}
		for _, repo := range repos.Repositories {
			groupRepos = append(groupRepos, repo.GetName())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return groupRepos
}

func (m *manager) commentAndSucceed(message string) {
	githubactions.Errorf("Sending message: %s", message)
	_, resp, err := m.client.Issues.CreateComment(m.ctx, m.org, m.repo, m.issueNumber, &github.IssueComment{
		Body: &message,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("Unable to send message, issue not found: %v", err)
			return
		}
		githubactions.Errorf("Unable to send message: %v", err)
		return
	}
}

func (m *manager) commentAndFail(message string) {
	githubactions.Errorf("Sending failure notification: %s", message)
	_, resp, err := m.client.Issues.CreateComment(m.ctx, m.org, m.repo, m.issueNumber, &github.IssueComment{
		Body: &message,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("Unable to send message, issue not found: %v", err)
			return
		}
		githubactions.Errorf("Unable to send message: %v", err)
		return
	}
	os.Exit(1)
}
