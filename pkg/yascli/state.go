package yascli

type stateCmd struct {
	Show *stateShowCmd `command:"show" description:"Show branch metadata"`
}
