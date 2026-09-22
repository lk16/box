package config

// The refusals box prints, kept together so a change to one is read next to the others.
const (
	MissingBinariesHelp = `these commands are not on PATH: %s.
box shells out to sbx to create the sandbox, to git to fetch the sandbox's work back, and to claude
to name the branch that work lands on, so all three have to be installed.`

	NoConfigHelp = `this project has no ` + ConfigFile + `, so box has no settings to run with.
Run box gen to write a starter one, then name a model in it.`

	KitHelp = `kit is not set, so the sandbox would run without a network policy.
Point it at a kit directory holding a spec.yaml in ` + ConfigFile + ` or with --kit; box gen writes a
starter one at ` + KitSpecFile + ` and points kit at it.`

	KitFileHelp = `kit names a file, and sbx reads anything that is not a directory as a zip
artifact. Point it at the directory holding spec.yaml, e.g. ` + KitDir + ` rather than
` + KitSpecFile + `.`

	ModelHelp = `model is not set, so the sandbox's own Claude version would pick the model.
That version need not match the one on this host. Name the model in ` + ConfigFile + ` or with --model.`

	MountsIgnoredHelp = MountsFile + ` is not ignored by git.
It names folders on this machine, so committing it would put paths that exist only here into
everyone else's clone. Add a ` + MountsFile + ` line to .gitignore.`

	ReposIgnoredHelp = ReposFile + ` is not ignored by git.
It names where each member sits on this machine, so committing it would put paths that exist only
here into everyone else's clone. Add a ` + ReposFile + ` line to .gitignore.`

	NotARepositoryHelp = `this is not a git repository.
box hands the agent a clone of this directory, so there has to be something here to clone. Run
git init and commit, then try again.`

	NoCommitsHelp = `this git repository has no commits.
box hands the agent a clone of this directory, and a repository with no commits clones to nothing
the agent can work from or branch off. Make at least one commit, then try again.`

	SbxTooOldHelp = `this sbx is v%s, and box needs v%s or newer.
The kits box writes use the spec layout that release introduced, which older ones reject. Upgrade
sbx, then try again.`

	VersionMismatchHelp = `the sbx client and its daemon are different versions: %s.
The daemon keeps running across an sbx upgrade, so the old one keeps answering until it is
restarted. Run sbx daemon restart, then try again.`

	TokenFileHelp = TokenFileEnv + ` is not set. Set it up once:
  1. Run: claude setup-token
  2. Save the printed token to a file, e.g. ~/.secrets/claude-oauth.token
  3. Export ` + TokenFileEnv + ` to point at that file, e.g. via direnv.`

	SecretsFileHelp = `%s declares ` + SecretHosts + `, but ` + SecretsEnv + ` is not set.
Set it up once:
  1. Write a file holding one NAME=value line per secret, e.g. ~/.secrets/box.env
  2. Export ` + SecretsEnv + ` to point at that file, e.g. via direnv.
Keep that file outside every repository and every mount, since the sandbox can read those.`

	MemberTemplateHelp = `%s sets template, and a sandbox runs one image.
A member cannot bring its own, so the template the group names has to cover every member of it.`

	MemberInsideHelp = `%s sits at %s, which is inside %s.
Its clone would sit inside the sandbox's own clone and leave it dirty, so a member has to live
outside the repository box runs in.`

	MemberMountedHelp = `%s sits at %s, which the mount %s would hand over whole.
A member reaches the sandbox as a bundle of its commits, never as a mount, so nothing it does not
track can go with it.`

	UnplacedMembersHelp = `%s is missing a path for:
%s
Clone each one this machine does not have yet, then put where it sits there; box gen adds every name.
Give a member null instead of a path to run without it on this machine.`

	OriginMismatchHelp = `%s sits at %s, whose origin is %s,
but ` + ConfigFile + ` gives it the ` + MemberOrigin + ` %s.
Point %s in %s at a clone of %s,
or fix ` + MemberOrigin + ` if the repository moved.`

	SecretInsideHelp = `%s points at %s, which is inside %s.
The sandbox can read everything there, so the file holding secrets has to sit somewhere else.`

	GroupQuestion = `Is this for one project, or for a group of projects?
  1  one project: the code in this folder
  2  a group: several projects next to this folder
Type 1 or 2 (Enter means 1): `

	GroupNextStep = `Next: list each project under "` + Repos + `" in ` + ConfigFile + `, like
  "api": {"` + MemberBranch + `": "main", "` + MemberOrigin + `": "git@example.com:team/api.git"}
where main is the branch to start from, then run box gen again and fill in where each one sits on
this machine in ` + ReposFile + `.`
)
