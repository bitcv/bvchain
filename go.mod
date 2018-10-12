module bvchain

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/btcsuite/btcd v0.0.0-20180924021209-2a560b2036be
	github.com/go-kit/kit v0.7.0
	github.com/go-logfmt/logfmt v0.3.0 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/snappy v0.0.0-20180518054509-2e65f85255db // indirect
	github.com/syndtr/goleveldb v0.0.0-20180815032940-ae2bd5eed72d
	golang.org/x/crypto v0.0.0
	gopkg.in/karalabe/cookiejar.v2 v2.0.0-20150724131613-8dcd6a7f4951

	halftwo/mangos v0.0.0
)

replace golang.org/x/crypto => github.com/golang/crypto v0.0.0-20181009213950-7c1a557ab941

replace halftwo/mangos => github.com/halftwo/mangos v0.0.0-20181010062021-c2dcb282643d
