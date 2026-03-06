module github.com/abhipray-cpu/go-audit/adapter/serializer/msgpack

go 1.21

require (
	github.com/abhipray-cpu/go-audit v0.0.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
)

require github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect

replace github.com/abhipray-cpu/go-audit => ../../..
