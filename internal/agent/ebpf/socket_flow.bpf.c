//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#define AF_INET 2
#define AF_INET6 10
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

struct flow_event {
    __u64 monotonic_ns;
    __u64 cgroup_id;
    __u32 pid;
    __u8 family;
    __u8 protocol;
    __u8 state;
    __u8 pad1;
    __be16 local_port;
    __be16 remote_port;
    __u8 local_addr[16];
    __u8 remote_addr[16];
    __u32 pad2;
    __u64 sent;
    __u64 received;
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24);
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 16384);
    __type(key, __u64);
    __type(value, struct flow_event);
} udp_receives SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 16384);
    __type(key, __u64);
    __type(value, struct flow_event);
} pending_sends SEC(".maps");

static __always_inline int fill_event(struct flow_event *event, struct sock *sk, __u8 protocol)
{
    __u16 local_port;
    __u32 local_v4;
    __u32 remote_v4;

    __builtin_memset(event, 0, sizeof(*event));
    event->monotonic_ns = bpf_ktime_get_ns();
    event->cgroup_id = bpf_get_current_cgroup_id();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->family = BPF_CORE_READ(sk, __sk_common.skc_family);
    event->protocol = protocol;
    event->state = BPF_CORE_READ(sk, __sk_common.skc_state);
    local_port = BPF_CORE_READ(sk, __sk_common.skc_num);
    event->local_port = bpf_htons(local_port);
    event->remote_port = BPF_CORE_READ(sk, __sk_common.skc_dport);

    if (event->family == AF_INET) {
        local_v4 = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
        remote_v4 = BPF_CORE_READ(sk, __sk_common.skc_daddr);
        __builtin_memcpy(event->local_addr, &local_v4, sizeof(local_v4));
        __builtin_memcpy(event->remote_addr, &remote_v4, sizeof(remote_v4));
    } else if (event->family == AF_INET6) {
        bpf_core_read(event->local_addr, sizeof(event->local_addr),
                      &sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr8);
        bpf_core_read(event->remote_addr, sizeof(event->remote_addr),
                      &sk->__sk_common.skc_v6_daddr.in6_u.u6_addr8);
    } else {
        return 0;
    }
    return 1;
}

static __always_inline int emit(struct sock *sk, __u8 protocol, __u64 sent, __u64 received)
{
    struct flow_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    if (!fill_event(event, sk, protocol)) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    event->sent = sent;
    event->received = received;
    bpf_ringbuf_submit(event, 0);
    return 0;
}

static __always_inline int save_send(struct sock *sk, __u8 protocol)
{
    __u64 id = bpf_get_current_pid_tgid();
    struct flow_event event;
    if (!fill_event(&event, sk, protocol))
        return 0;
    bpf_map_update_elem(&pending_sends, &id, &event, BPF_ANY);
    return 0;
}

static __always_inline int emit_send(int copied)
{
    __u64 id = bpf_get_current_pid_tgid();
    struct flow_event *saved = bpf_map_lookup_elem(&pending_sends, &id);
    struct flow_event *event;
    if (!saved)
        return 0;
    if (copied > 0) {
        event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
        if (event) {
            __builtin_memcpy(event, saved, sizeof(*event));
            event->sent = copied;
            bpf_ringbuf_submit(event, 0);
        }
    }
    bpf_map_delete_elem(&pending_sends, &id);
    return 0;
}

SEC("kprobe/tcp_sendmsg")
int BPF_KPROBE(trace_tcp_sendmsg, struct sock *sk, struct msghdr *msg, size_t size)
{
    return save_send(sk, IPPROTO_TCP);
}

SEC("kretprobe/tcp_sendmsg")
int BPF_KRETPROBE(trace_tcp_sendmsg_return, int copied)
{
    return emit_send(copied);
}

SEC("kprobe/tcp_cleanup_rbuf")
int BPF_KPROBE(trace_tcp_cleanup_rbuf, struct sock *sk, int copied)
{
    if (copied > 0)
        return emit(sk, IPPROTO_TCP, 0, copied);
    return 0;
}

SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(trace_udp_sendmsg, struct sock *sk, struct msghdr *msg, size_t len)
{
    return save_send(sk, IPPROTO_UDP);
}

SEC("kretprobe/udp_sendmsg")
int BPF_KRETPROBE(trace_udp_sendmsg_return, int copied)
{
    return emit_send(copied);
}

SEC("kprobe/udp_recvmsg")
int BPF_KPROBE(trace_udp_recvmsg, struct sock *sk)
{
    __u64 id = bpf_get_current_pid_tgid();
    struct flow_event event;
    if (!fill_event(&event, sk, IPPROTO_UDP))
        return 0;
    bpf_map_update_elem(&udp_receives, &id, &event, BPF_ANY);
    return 0;
}

SEC("kretprobe/udp_recvmsg")
int BPF_KRETPROBE(trace_udp_recvmsg_return, int copied)
{
    __u64 id = bpf_get_current_pid_tgid();
    struct flow_event *saved = bpf_map_lookup_elem(&udp_receives, &id);
    struct flow_event *event;
    if (!saved)
        return 0;
    if (copied <= 0) {
        bpf_map_delete_elem(&udp_receives, &id);
        return 0;
    }
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        __builtin_memcpy(event, saved, sizeof(*event));
        event->received = copied;
        bpf_ringbuf_submit(event, 0);
    }
    bpf_map_delete_elem(&udp_receives, &id);
    return 0;
}

char LICENSE[] SEC("license") = "Dual MIT/GPL";
