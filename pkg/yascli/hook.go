package yascli

type hookCmd struct {
	Bash *hookBashCmd `command:"bash" description:"Print bash shell hook"`
	Zsh  *hookZshCmd  `command:"zsh"  description:"Print zsh shell hook"`
}
