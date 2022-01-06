// TODO: Add log URL to failures

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v41/github"
	"github.com/sethvargo/go-githubactions"
	"golang.org/x/oauth2"
)

type manager struct {
	ctx    context.Context
	client *github.Client

	action         string
	actor          string
	authorizedTeam string
	body           string
	issueNumber    int
	org            string
	repo           string
	runnerGroup    string
	runnerGroupID  int64
	secrets        []string
	team           string
	workflowRunID  int64
}

func main() {
	action := githubactions.GetInput("action")
	actor := githubactions.GetInput("actor")
	authorizedTeam := githubactions.GetInput("authorized_team")
	body := githubactions.GetInput("body")
	body = strings.Replace(body, "\r", "", -1)
	org := githubactions.GetInput("org")
	repo := githubactions.GetInput("repo")
	token := githubactions.GetInput("token")
	issueNumber, err := strconv.Atoi(githubactions.GetInput("issue_number"))
	if err != nil {
		githubactions.Fatalf("Failed to parse issue number: %v", err)
	}
	workflowRunID, err := strconv.Atoi(githubactions.GetInput("workflow_run_id"))
	if err != nil {
		githubactions.Fatalf("Failed to parse workflow run ID: %v", err)
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	manager := &manager{
		ctx:    ctx,
		client: client,

		action:         action,
		actor:          actor,
		authorizedTeam: authorizedTeam,
		body:           body,
		issueNumber:    issueNumber,
		org:            org,
		repo:           repo,
		workflowRunID:  int64(workflowRunID),
	}
	manager.team, err = manager.retrieveTeam()
	if err != nil {
		githubactions.Fatalf("Failed to retrieve team: %v", err)
	}
	manager.secrets = []string{manager.team, manager.runnerGroup}
	manager.runnerGroup = fmt.Sprintf("ghm-%s", manager.team)

	githubactions.AddMask(manager.runnerGroup)

	if !manager.verifyAuthorization() {
		githubactions.Fatalf("Authorization failed, user is not authorized to perform this actions")
	}

	if !manager.verifyTeamExists() {
		manager.commentAndFail("Unable to verify team exists")
	}

	if !manager.verifyMaintainership() {
		manager.commentAndFail("Unable to verify you are a maintainer of this team")
	}

	if action != "group-create" {
		var found bool
		manager.runnerGroupID, found = manager.retrieveRunnerGroupID()
		if !found {
			manager.commentAndFail("Failed to retrieve runner group ID")
		}
	}

	githubactions.Infof("Executing action %s for %s/%s", action, manager.org, actor)
	switch action {
	case "group-create":
		githubactions.Infof("Creating runner group")
		manager.createGroup()
		manager.commentAndSucceed("Created runner group. The name of your runner group is of the form `ghm-<team slug>`")
	case "group-delete":
		githubactions.Infof("Deleting runner group")
		manager.deleteGroup()
		manager.commentAndSucceed("Deleted runner group")
	case "group-list":
		githubactions.Infof("Listing runner group contents")
		repos, runners := manager.groupList()
		list := generateList(repos, runners)
		manager.commentAndSucceed("The following repos and runners are assigned to the runner group:\n\n%s", list)
	case "repos-add":
		githubactions.Infof("Adding repos to runner group")
		manager.addRepos()
		manager.commentAndSucceed("Added repos to runner group")
	case "repos-remove":
		githubactions.Infof("Removing repos from runner group")
		manager.removeRepos()
		manager.commentAndSucceed("Removed repos from runner group")
	case "repos-set":
		githubactions.Infof("Setting repos in runner group")
		manager.setRepos()
		manager.commentAndSucceed("Replaced repos in runner group")
	case "token-register":
		githubactions.Infof("Creating registration token")
		token := manager.createRegistrationToken()
		manager.commentAndSucceed("Created registration token\n\nToken: `%s`\nExpiration: `%s`\n\nRun the following command to configure your runner:\n\n```\n./config.sh --url https://github.com/%s --token %s\n```", token.GetToken(), token.GetExpiresAt().String(), manager.org, token.GetToken())
	case "token-remove":
		githubactions.Infof("Creating removal token")
		token := manager.createRemovalToken()
		manager.commentAndSucceed("Created removed token\n\nToken: `%s`\nExpiration: `%s`\n\nRun the following command to configure your runner:\n\n```\n./config.sh --url https://github.com/%s --token %s\n```", token.GetToken(), token.GetExpiresAt().String(), manager.org, token.GetToken())
	}
}

func (m *manager) createGroup() {
	group, _, err := m.client.Actions.CreateOrganizationRunnerGroup(m.ctx, m.org, github.CreateRunnerGroupRequest{
		AllowsPublicRepositories: github.Bool(false),
		Name:                     &m.runnerGroup,
		Visibility:               github.String("selected"),
	})
	if err != nil {
		m.commentAndFail("Unable to create group: %v", err)
	}
	githubactions.Infof("Created group %s", group.GetName())
}

func (m *manager) deleteGroup() {
	_, err := m.client.Actions.DeleteOrganizationRunnerGroup(m.ctx, m.org, m.runnerGroupID)
	if err != nil {
		m.commentAndFail("Unable to delete group: %v", err)
	}
	githubactions.Infof("Deleted group")
}

func (m *manager) groupList() ([]string, []string) {
	repos := m.retrieveRunnerGroupRepos()
	runners := m.retrieveRunnerGroupRunners()
	return repos, runners
}

func (m *manager) addRepos() {
	repos := make(map[string]int64)
	for _, repo := range m.parseRepos() {
		m.verifyRepoAssignedToTeam(repo)
		id := m.retrieveRepoID(repo)
		repos[repo] = id
	}

	for name, id := range repos {
		githubactions.Infof("Adding repo %s to group")
		_, err := m.client.Actions.AddRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, id)
		if err != nil {
			m.commentAndFail("Unable to add repo %d to group: %v", name, err)
		}
	}
}

func (m *manager) removeRepos() {
	repos := make(map[string]int64)
	for _, repo := range m.parseRepos() {
		m.verifyRepoAssignedToTeam(repo)
		id := m.retrieveRepoID(repo)
		repos[repo] = id
	}

	for name, id := range repos {
		githubactions.Infof("Removing repo %s from group", name)
		_, err := m.client.Actions.RemoveRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, id)
		if err != nil {
			m.commentAndFail("Unable to remove repo %s to group: %v", name, err)
		}
	}
}

func (m *manager) setRepos() {
	repos := m.parseRepos()
	var repoIDs []int64
	for _, repo := range repos {
		m.verifyRepoAssignedToTeam(repo)
		id := m.retrieveRepoID(repo)
		repoIDs = append(repoIDs, id)
	}

	githubactions.Infof("Replacing existing repos for group with new repo set: [%s]", strings.Join(repos[:], ", "))
	_, err := m.client.Actions.SetRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, github.SetRepoAccessRunnerGroupRequest{SelectedRepositoryIDs: repoIDs})
	if err != nil {
		m.commentAndFail("Unable to replace repos for group: %v", err)
	}
}

func (m *manager) createRegistrationToken() *github.RegistrationToken {
	token, _, err := m.client.Actions.CreateOrganizationRegistrationToken(m.ctx, m.org)
	if err != nil {
		m.commentAndFail("Unable to create registration token: %v", err)
	}
	githubactions.Infof("Created registration token")
	return token
}

func (m *manager) createRemovalToken() *github.RemoveToken {
	token, _, err := m.client.Actions.CreateOrganizationRemoveToken(m.ctx, m.org)
	if err != nil {
		m.commentAndFail("Unable to create removal token: %v", err)
	}
	githubactions.Infof("Created removal token")
	return token
}

func (m *manager) verifyAuthorization() bool {
	githubactions.Infof("Verifying user is authorized to perform this action")
	opts := github.TeamListTeamMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		members, resp, err := m.client.Teams.ListTeamMembersBySlug(m.ctx, m.org, m.authorizedTeam, &opts)
		if err != nil {
			m.commentAndFail("Unable to list team members: %v", err)
		}
		for _, member := range members {
			if member.GetLogin() == m.actor {
				return true
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return false
}

func (m *manager) verifyMaintainership() bool {
	githubactions.Infof("Verifying %s is a maintainer of the team")
	membership, resp, err := m.client.Teams.GetTeamMembershipBySlug(m.ctx, m.org, m.team, m.actor)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("%s is not a member of the team", m.actor)
			return false
		}
		githubactions.Errorf("Unable to get team membership for %s: %v", m.actor, err)
		return false
	}
	if membership.GetRole() != "maintainer" {
		githubactions.Errorf("%s is not a maintainer of the team", m.actor)
		return false
	}
	return true
}

func (m *manager) verifyTeamExists() bool {
	githubactions.Infof("Verifying team exists")
	team, resp, err := m.client.Teams.GetTeamBySlug(m.ctx, m.org, m.team)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("Team does not exist")
			return false
		}
		githubactions.Errorf("Unable to get team: %v", err)
		return false
	}
	githubactions.Infof("Verifying team privacy is set to secret")
	if team.GetPrivacy() != "secret" {
		m.commentAndFail("Team is not private")
	}
	githubactions.Infof("Team exists")
	return true
}

func (m *manager) retrieveRunnerGroupID() (int64, bool) {
	githubactions.Infof("Searching for group ID for runner group")
	opts := &github.ListOptions{
		PerPage: 100,
	}
	for {
		groups, resp, err := m.client.Actions.ListOrganizationRunnerGroups(m.ctx, m.org, opts)
		if err != nil {
			m.commentAndFail("Unable to retrieve runner groups: %v", err)
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

func (m *manager) parseRepos() []string {
	r := regexp.MustCompile("Repos.+[A-Za-z0-9_.-]")
	match := r.FindStringSubmatch(m.body)[0]
	match = strings.TrimPrefix(match, "Repos")
	match = strings.Trim(match, "\\t \\r \\n")
	repos := strings.Split(match, ",")
	repos = m.trimRepoNames(repos)
	return repos
}

func (m *manager) retrieveTeam() (string, error) {
	switch m.action {
	case "group-create", "group-delete", "group-list", "token-register", "token-remove":
		return m.parseTeam(), nil
	case "repos-add", "repos-remove", "repos-set":
		return m.parseTeamWithRepos(), nil
	default:
		return "", fmt.Errorf("unable to parse team from body")
	}
}

func (m *manager) parseTeam() string {
	r := regexp.MustCompile("Team.+[A-Za-z0-9_.-]")
	match := r.FindStringSubmatch(m.body)[0]
	match = strings.TrimPrefix(match, "Team")
	match = strings.Trim(match, "\\t \\r \\n")
	githubactions.AddMask(match)
	return match
}

func (m *manager) parseTeamWithRepos() string {
	r := regexp.MustCompile("Team.+###")
	match := r.FindStringSubmatch(m.body)[0]
	match = strings.TrimPrefix(match, "Team")
	match = strings.Trim(match, "\\t \\r \\n #")
	githubactions.AddMask(match)
	return match
}

func (m *manager) trimRepoNames(repos []string) []string {
	var trimmedRepos []string
	for _, repo := range repos {
		repo = strings.Trim(repo, "\t \r \n")
		trimmedRepos = append(trimmedRepos, repo)
		m.secrets = append(m.secrets, repo)
		githubactions.AddMask(repo)
	}
	return trimmedRepos
}

func (m *manager) retrieveRepoID(repoName string) int64 {
	githubactions.Infof("Verifying repo %s exists", repoName)
	repo, resp, err := m.client.Repositories.Get(m.ctx, m.org, repoName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			m.commentAndFail("Repo %s does not exist", repoName)
		}
		m.commentAndFail("Unable to get repository: %v", err)
	}
	return repo.GetID()
}

func (m *manager) retrieveRunnerGroupRunners() []string {
	githubactions.Infof("Retrieving runners for group")
	opts := &github.ListOptions{
		PerPage: 100,
	}
	var groupRunners []string
	for {
		runners, resp, err := m.client.Actions.ListRunnerGroupRunners(m.ctx, m.org, m.runnerGroupID, opts)
		if err != nil {
			m.commentAndFail("Unable to retrieve runners: %v", err)
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
	githubactions.Infof("Retrieving repos for runner group")
	opts := &github.ListOptions{
		PerPage: 100,
	}
	var groupRepos []string
	for {
		repos, resp, err := m.client.Actions.ListRepositoryAccessRunnerGroup(m.ctx, m.org, m.runnerGroupID, opts)
		if err != nil {
			m.commentAndFail("Unable to retrieve repos: %v", err)
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

func (m *manager) verifyRepoAssignedToTeam(name string) {
	githubactions.Infof("Verifying repo %s is assigned to team")
	opts := &github.ListOptions{
		PerPage: 100,
	}
	for {
		repos, resp, err := m.client.Teams.ListTeamReposBySlug(m.ctx, m.org, m.team, opts)
		if err != nil {
			m.commentAndFail("Unable to retrieve repos: %v", err)
		}
		for _, repo := range repos {
			if repo.GetName() == name {
				githubactions.Infof("Repo %s is assigned to team", name)
				return
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	m.commentAndFail("Repo %s is not assigned to team", name)
}

func generateList(repos, runners []string) string {
	builder := "```\nRepos:\n"
	if len(repos) == 0 {
		builder += "None\n"
	} else {
		for _, repo := range repos {
			builder += fmt.Sprintf("%s\n", repo)
		}
	}

	builder += "\n\nRunners:\n"
	if len(runners) == 0 {
		builder += "None\n"
	} else {
		for _, runner := range runners {
			builder += fmt.Sprintf("%s\n", runner)
		}
	}
	builder += "```"
	return builder
}

func (m *manager) commentAndSucceed(message string, args ...interface{}) {
	formattedMessage := fmt.Sprintf(message, args...)
	formattedMessage = redact(formattedMessage)
	githubactions.Infof("Sending message: %s", formattedMessage)
	_, resp, err := m.client.Issues.CreateComment(m.ctx, m.org, m.repo, m.issueNumber, &github.IssueComment{
		Body: &formattedMessage,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("Unable to send message, issue not found: %v", err)
			return
		}
		githubactions.Errorf("Unable to send message: %v", err)
	}
	m.closeIssue()
}

func (m *manager) commentAndFail(message string, args ...interface{}) {
	formattedMessage := fmt.Sprintf(message, args...)
	formattedMessage = formattedMessage + fmt.Sprintf("\n\n[View Failure Log Here](https://github.com/%s/%s/actions/runs/%d)", m.org, m.repo, m.workflowRunID)
	formattedMessage = redact(formattedMessage)
	githubactions.Warningf("Sending failure notification: %s", formattedMessage)
	_, resp, err := m.client.Issues.CreateComment(m.ctx, m.org, m.repo, m.issueNumber, &github.IssueComment{
		Body: &formattedMessage,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			githubactions.Errorf("Unable to send message, issue not found: %v", err)
			os.Exit(1)
		}
		githubactions.Errorf("Unable to send message: %v", err)
	}
	os.Exit(1)
}

func (m *manager) closeIssue() {
	githubactions.Infof("Closing issue %d", m.issueNumber)
	_, _, err := m.client.Issues.Edit(m.ctx, m.org, m.repo, m.issueNumber, &github.IssueRequest{
		State: github.String("closed"),
	})
	if err != nil {
		githubactions.Errorf("Unable to close issue: %v", err)
	}
}

func redact(text string) string {
	for _, word := range m.secrets {
		text = strings.ReplaceAll(text, word, "****")
	}
}
