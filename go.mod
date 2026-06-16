module github.com/juliaogris/teleport-expressions

go 1.25

require (
	github.com/gravitational/trace v1.5.4
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/stretchr/testify v1.8.3
	github.com/vulcand/predicate v1.3.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
)

replace github.com/vulcand/predicate => github.com/gravitational/predicate v1.4.0
