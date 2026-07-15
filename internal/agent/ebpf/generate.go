package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror -D__TARGET_ARCH_arm64" socketFlow socket_flow.bpf.c -- -I/usr/include/aarch64-linux-gnu
