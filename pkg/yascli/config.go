package yascli

type configCmd struct {
	Set  *configSetCmd  `command:"set" description:"Update/set a config value"`
	Show *configShowCmd `command:"show" description:"Show current configuration"`
}
