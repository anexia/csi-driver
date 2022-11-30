# Guidance on how to contribute

> By submitting a pull request or filing a bug, issue, or feature request,
> you are agreeing to comply with this waiver of copyright interest.
> Details can be found in our [LICENSE](LICENSE).

There are two primary ways to help:
 - Using the issue tracker, and
 - Changing the code-base.

If you found something you believe is a (possible) security issue, please do not hesitate
to contact us via the information in [SECURITY.md](SECURITY.md). Do not open an issue for
that, as it would be public and put users at risk.


## Using the issue tracker

Use the issue tracker to suggest feature requests, report bugs, and ask questions.
This is also a great way to connect with the developers of the project as well
as others who are interested in this solution.

You can also use it to find a bug to fix or feature to implement. Mention in
the issue that you want to work on it (to prevent multiple people working on the same),
then follow the _Changing the code-base_ guidance below.


## Changing the code-base

Generally speaking, you should fork this repository, make changes in your
own fork, and then submit a pull request. People having commit access on this repository can
also push their branches in this repository instead of a fork, but still have to open pull
requests to have things merged into `main`.

All new code should have associated unit tests that validate implemented features
and the presence or lack of defects. For tests we use [`ginkgo`](https://onsi.github.io/ginkgo/)
and [`gomega`](https://onsi.github.io/gomega/).

Code in this project follows the standard go code style (`go fmt`), our CI system enforces it.


### pre-commit hook

We use [`pre-commit`](https://pre-commit.com/) to run some things before you make a commit, making sure
your code is clean before even added to your local history. It's probably available in your systems package
manager, see the link for install instructions. When installed, run `pre-commit install` in the cloned
csi-driver sources to to activate it for you.

In its documentation you also find help if you have to skip a check for some reason.

## Testing

We use `Ginkgo` (v2) for our unit tests, though there are some older ones. Unit tests are located directly in the
package they test. Tests are executed with `make test`.
