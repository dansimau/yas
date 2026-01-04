package yascli

type completionCmd struct {
	Bash *completionBashCmd `command:"bash" description:"Generate bash completion script"`
	Zsh  *completionZshCmd  `command:"zsh"  description:"Generate zsh completion script"`
	Fish *completionFishCmd `command:"fish" description:"Generate fish completion script"`
}
