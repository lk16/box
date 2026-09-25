package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/lk16/box/internal/fail"
)

// Member is one repository a group works on besides the one box runs in, and where it sits here.
type Member struct {
	Name      string
	Branch    string
	GitOrigin string
	Path      string
	// Missing is what a null in the repos file says: this machine does not have this member.
	Missing bool
}

// MemberSettings is what a member's own .box/ adds to the session it is part of.
type MemberSettings struct {
	Member     Member
	Mounts     []string
	Kit        string
	PromptFile string
}

// memberKeys are the two fields a member is declared with, and the only ones box reads.
var memberKeys = []string{MemberBranch, MemberOrigin}

// declaredText reads one field a member is declared with, rejecting one that is missing or empty.
func declaredText(name string, declaration map[string]json.RawMessage, key string) (string, error) {
	value := ""
	if raw, given := declaration[key]; given {
		text, err := asText(ConfigFile, Repos+" "+name+" "+key, raw)
		if err != nil {
			return "", err
		}
		value = text
	}
	if value == "" {
		return "", fail.Errorf("%s gives %s no %s", Repos, name, key)
	}
	return value, nil
}

// ToMember takes one declared member, placed wherever this machine's repos file says it sits.
func ToMember(name string, raw json.RawMessage, paths Pairs) (Member, error) {
	if name == "" {
		return Member{}, fail.Errorf("%s names a member with no name", Repos)
	}
	declaration, ok := asObject(raw)
	if !ok {
		return Member{}, fail.Errorf("%s gives %s %s, where %s belongs", Repos, name, typeName(raw), MemberShape)
	}
	if err := rejectUnknownMemberKeys(name, declaration); err != nil {
		return Member{}, err
	}
	// origin/HEAD is written when a clone is made and goes stale, so the branch is named here.
	branch, err := declaredText(name, declaration, MemberBranch)
	if err != nil {
		return Member{}, err
	}
	origin, err := declaredText(name, declaration, MemberOrigin)
	if err != nil {
		return Member{}, err
	}
	return Member{
		Name: name, Branch: branch, GitOrigin: origin,
		Path: paths.Get(name), Missing: paths.Null(name),
	}, nil
}

// rejectUnknownMemberKeys refuses a declaration holding a field box would silently ignore.
func rejectUnknownMemberKeys(name string, declaration map[string]json.RawMessage) error {
	var unknown []string
	for key := range declaration {
		if !slices.Contains(memberKeys, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fail.Errorf("%s gives %s unknown keys: %s", Repos, name, strings.Join(unknown, ", "))
}

// ToMembers normalises the repos value into this group's members, sorted by name.
func ToMembers(raw json.RawMessage, paths Pairs) ([]Member, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := asObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must be a JSON object of name to %s", Repos, MemberShape)
	}
	members := make([]Member, 0, len(object))
	for _, name := range sortedKeys(object) {
		member, err := ToMember(name, object[name], paths)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

// isScpOrigin says whether a URL is git's scp-like host:path, which git reads only with no slash first.
func isScpOrigin(url string) bool {
	before, _, found := strings.Cut(url, ":")
	return found && !strings.Contains(before, "/")
}

// splitOrigin splits a remote URL into who it reaches and where, and the path of the repository there.
func splitOrigin(url string) (string, string) {
	if _, after, found := strings.Cut(url, "://"); found {
		authority, path, _ := strings.Cut(after, "/")
		return authority, path
	}
	authority, path, _ := strings.Cut(url, ":")
	return authority, path
}

// NormalizeOrigin reduces an origin to host and path, so every spelling of one repository agrees.
func NormalizeOrigin(url string) string {
	text := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git")
	// A local path names a directory on this disk, which has no other spelling to agree with.
	if !strings.Contains(text, "://") && !isScpOrigin(text) {
		return text
	}
	authority, path := splitOrigin(text)
	// A user, a password and a port say how to reach a host, so the host follows the last @.
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	host, _, _ := strings.Cut(authority, ":")
	return strings.ToLower(host) + "/" + strings.Trim(path, "/")
}

// RequireDistinctOrigins refuses two members that are one repository, whose work would collide.
func RequireDistinctOrigins(members []Member) error {
	seen := map[string]string{}
	for _, member := range members {
		origin := NormalizeOrigin(member.GitOrigin)
		if first, taken := seen[origin]; taken {
			return fail.Errorf("%s gives %s and %s one %s, and two clones of one repository would bring their work back over each other",
				Repos, first, member.Name, MemberOrigin)
		}
		seen[origin] = member.Name
	}
	return nil
}

// DescribeMembers lists members by name, each with the origin to clone it from.
func DescribeMembers(members []Member) string {
	lines := make([]string, 0, len(members))
	for _, member := range members {
		lines = append(lines, "  "+member.Name+": "+member.GitOrigin)
	}
	return strings.Join(lines, "\n")
}

// asPlacements reads a repos file's values, where a null stands for a member this machine has none of.
func asPlacements(path string, object map[string]json.RawMessage) (Pairs, error) {
	placements := make(Pairs, 0, len(object))
	for _, name := range sortedKeys(object) {
		// A null is the one answer that is no path: this machine does not have this member at all.
		if decode(object[name]) == nil {
			placements = append(placements, Pair{Name: name, Null: true})
			continue
		}
		text, err := asText(path, name, object[name])
		if err != nil {
			return nil, err
		}
		placements = append(placements, Pair{Name: name, Value: text})
	}
	return placements, nil
}

// ReadReposFile reads where each member sits on this machine, or nothing when there is no file.
func ReadReposFile(path string) (Pairs, error) {
	object, err := readNamedPaths(path)
	if object == nil || err != nil {
		return nil, err
	}
	return asPlacements(path, object)
}

// RequirePlacedMembers refuses a repos file that does not give every declared member a path, and only those.
func RequirePlacedMembers(members []Member, paths Pairs, reposFile string) error {
	var unknown []string
	for _, pair := range paths {
		if !slices.ContainsFunc(members, func(m Member) bool { return m.Name == pair.Name }) {
			unknown = append(unknown, pair.Name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fail.Errorf("%s names members %s does not declare: %s", reposFile, ConfigFile, strings.Join(unknown, ", "))
	}
	var unplaced []Member
	for _, member := range members {
		// A member answered with a null is placed: this machine deliberately does not have it.
		if member.Path == "" && !member.Missing {
			unplaced = append(unplaced, member)
		}
	}
	if len(unplaced) == 0 {
		return nil
	}
	return fail.Errorf(UnplacedMembersHelp, reposFile, DescribeMembers(unplaced))
}

// ReadRepos reads the declared members, each placed where this machine's repos file says it sits.
func ReadRepos(workingDirectory string, raw json.RawMessage) ([]Member, error) {
	reposFile := filepath.Join(workingDirectory, ReposFile)
	paths, err := ReadReposFile(reposFile)
	if err != nil {
		return nil, err
	}
	members, err := ToMembers(raw, paths)
	if err != nil {
		return nil, err
	}
	if err := RequireDistinctOrigins(members); err != nil {
		return nil, err
	}
	return members, RequirePlacedMembers(members, paths, reposFile)
}

// MemberPath is where a member sits on this host, which is where its clone sits in the sandbox.
func MemberPath(workingDirectory string, member Member) string {
	return Resolve(Under(workingDirectory, ResolvePath(member.Path)))
}

// memberKit resolves a member's kit against the member itself, since its path is written relative to it.
func memberKit(directory, kit string) string {
	if kit == "" {
		return ""
	}
	path := Under(directory, ResolvePath(kit))
	// A kit that is not on disk is a reference sbx resolves itself, and no path of this machine's.
	if _, err := os.Stat(path); err != nil {
		return kit
	}
	return path
}

// ReadMemberSettings reads what a member's own .box/ adds, which is nothing at all when it has none.
func ReadMemberSettings(workingDirectory string, member Member) (MemberSettings, error) {
	// A member this machine does not have sits nowhere, so there is no .box/ of its own to read.
	if member.Missing {
		return MemberSettings{Member: member}, nil
	}
	directory := MemberPath(workingDirectory, member)
	path := filepath.Join(directory, ConfigFile)
	// A member with no box setup of its own is a repository to clone and nothing more.
	if !IsFile(path) {
		return MemberSettings{Member: member}, nil
	}
	values, err := ReadConfigFile(path)
	if err != nil {
		return MemberSettings{}, err
	}
	if values.Setting("template") != "" {
		return MemberSettings{}, fail.Errorf(MemberTemplateHelp, path)
	}
	mounts, err := memberMounts(directory, member, values)
	if err != nil {
		return MemberSettings{}, err
	}
	promptFile := values.Setting("prompt_file")
	if promptFile != "" {
		promptFile = Under(directory, ResolvePath(promptFile))
	}
	return MemberSettings{
		Member:     member,
		Mounts:     mounts,
		Kit:        memberKit(directory, values.Setting("kit")),
		PromptFile: promptFile,
	}, nil
}

// memberMounts answers a member's own declaration from its own mounts file, naming it in every message.
func memberMounts(directory string, member Member, values Values) ([]string, error) {
	required, err := AsDescriptions(values.Raw[RequiredMounts])
	if err != nil {
		return nil, err
	}
	provided, err := ReadPathsFile(filepath.Join(directory, MountsFile))
	if err != nil {
		return nil, err
	}
	return OrderMounts(Scoped(member.Name, required), Scoped(member.Name, provided))
}

// ReadMembers reads every member's own settings, in the order the members come.
func ReadMembers(workingDirectory string, members []Member) ([]MemberSettings, error) {
	all := make([]MemberSettings, 0, len(members))
	for _, member := range members {
		settings, err := ReadMemberSettings(workingDirectory, member)
		if err != nil {
			return nil, err
		}
		all = append(all, settings)
	}
	return all, nil
}
