# TODO

* [ ] Add `branch` command for switching branches interactively
* [ ] Add merge command that merges the PR (and removes the annotation from the PR body)
* [ ] Add move command
* [ ] Don't allow `yas add` when it's already added, except with --force. Use move instead.
* [ ] Fix PushBranch to not hardcode origin
* [ ] Parallel operations where possible:
    -> Annotation
    -> Submit? Can we do a git push in parallel?
* Add commit command that summarises the PR using AI
