package devtool

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilePortAllocatorSkipsLeasedPorts(t *testing.T) {
	t.Parallel()

	allocator := &FilePortAllocator{LockRoot: t.TempDir()}
	preferred := freePort(t)

	first, err := allocator.Acquire(preferred)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	second, err := allocator.Acquire(first.Port())
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	assert.Greater(t, second.Port(), first.Port(), "a leased port isn't handed out twice")

	require.NoError(t, first.Close())
	third, err := allocator.Acquire(first.Port())
	require.NoError(t, err)
	t.Cleanup(func() { _ = third.Close() })
	assert.Equal(t, first.Port(), third.Port(), "a released port can be leased again")
}

func TestFilePortAllocatorSkipsPortsHeldOnAnyAddress(t *testing.T) {
	t.Parallel()

	// Another project's dev server may hold the port on any of these. tcp4
	// and tcp6 keep each holder to one address family, since "tcp" on
	// 0.0.0.0 opens a dual-stack socket and would hide an IPv4-only holder.
	// One bound to only ::1 matters too: the browser resolves localhost to
	// ::1 first, so it would load that project instead of ours.
	holders := []struct{ network, host string }{
		{"tcp4", "127.0.0.1"},
		{"tcp4", "0.0.0.0"},
		{"tcp6", "::1"},
		{"tcp6", "::"},
	}
	for _, holder := range holders {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), holder.network, net.JoinHostPort(holder.host, "0"))
		if err != nil {
			t.Logf("skipping %s %s: %v", holder.network, holder.host, err)
			continue
		}
		occupied := listener.Addr().(*net.TCPAddr).Port

		lease, err := (&FilePortAllocator{LockRoot: t.TempDir()}).Acquire(occupied)
		require.NoError(t, err)
		assert.NotEqual(t, occupied, lease.Port(), "port held on %s %s", holder.network, holder.host)

		require.NoError(t, lease.Close())
		require.NoError(t, listener.Close())
	}
}

func TestPortLeasedByRecognizesTheOwner(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lease, err := (&FilePortAllocator{LockRoot: root, Owner: "/worktrees/feature"}).Acquire(freePort(t))
	require.NoError(t, err)

	assert.True(t, portLeasedBy(root, lease.Port(), "/worktrees/feature"))
	assert.False(t, portLeasedBy(root, lease.Port(), "/worktrees/other"))

	require.NoError(t, lease.Close())
	assert.False(t, portLeasedBy(root, lease.Port(), "/worktrees/feature"), "a released lease has no owner")
}

// freePort returns a port nothing is listening on, so tests don't depend on
// which well-known ports this machine happens to use.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	require.True(t, portAvailable(port), "port "+strconv.Itoa(port)+" should be free")
	return port
}
