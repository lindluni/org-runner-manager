# Organization Runner Manager

Organization runner manager is a set of GitHub Actions that allows non-admin users to manage organization runners. 
Users will open issues and fill out the issue form to manage the runner group. The following API's are exposed:

 - `group-create` - Create a new organization runner group
 - `group-delete` - Delete an existing organization runner group
 - `group-list` - List all repos and runners assigned to a runner group
 - `repos-add` - Add one or more repos to an organization runner group
 - `repos-remove` - Remove one or more repos from an organization runner group
 - `repos-set` - Replace all existing repos in an organization runner group with a new set of repos
 - `token-register` - Create a new organization runner registration token
 - `token-remove` - Create a new organization runner removal token

## Configuration

In order to use these sets of Actions you first must create a **Secret** GitHub team. You can navigate to the [teams page]() 
and create a new **Secret** team. Be sure make the team `Secret` and not `Public` when you create the team.

The user who is going to open the issues, must be a maintainer of the team. You must also add the repositories you intend
to add to the runner group to the team. The repositories need only read access. You can do this on the team page by 
navigating to the **Repositories** tab.

Once you've created a team, you must be added to the authorized users list. You can do this by opening an [Authorize User issue]()

Once you've opened the issue, a member of our team will review your request and add you to the authorized users list, after
verifying you have completed all the steps above.

## Managing Organization Runner Groups

A normal flow for creating a new organization runner group and adding repos and runners is as follows:

- Create a new organization runner group by opening a [Create New Group Issue]()
- Add repos to the group by opening a [Add Repos to Runner Group Issue]()
- Add runners to the group by generating registration tokens by opening a [Create Organization Runner Registration Token Issue]()
- If you want to list the repos and runners assigned to your runner group, you can do so by opening a [List Runner Group Contents Issue]()
- If you want to remove repos from your runner group, you can do so by opening a [Remove Repos from Runner Group Issue]()
- If you want to replace all repos in your runner group, with a new set of repos, you can do so by opening a [Replace All Repos in Runner Group Issue]()
- If you want to remove a runner from a runner group, generate a removal token by opening a [Create Organization Runner Removal Token Issue]()
- If you need to delete a group, you can do so by opening a [Delete Existing Group Issue]()
