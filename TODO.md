# TODO

* [ ] Convert annotation to be a comment
  * Post comment: `gh pr comment "$pr_number" --body "Test comment" --json id -q .id`
  * Post comment: `gh api --method POST repos/:owner/:repo/issues/:pr_number/comments -f body='Your comment text here' --jq '.id'`
  * Modify comment: `gh api --method PATCH /repos/$(gh repo view --json owner,name -q '.owner.login + "/" + .name')/issues/comments/<COMMENT_ID> -f body="$(cat)"`
* [ ] Add merge command that merges the PR (and removes the annotation from the PR body)
* [ ] Add move command
* [ ] Don't allow `yas add` when it's already added, except with --force. Use move instead.
* [ ] Parallel operations where possible:
    -> Annotation
    -> Submit? Can we do a git push in parallel?
* Add commit command that summarises the PR using AI
