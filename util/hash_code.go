package util

import (
	"hash/fnv"
	"strconv"
)

func Hash(s string) string {
	hasher := fnv.New32a()
	hasher.Write([]byte(s))
	h := hasher.Sum32()
	return strconv.FormatUint(uint64(h), 10)
}
