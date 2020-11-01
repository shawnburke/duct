package duct

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	dc "github.com/fsouza/go-dockerclient"
	"golang.org/x/sys/unix"
)

func TestBasic(t *testing.T) {
	c := New(Manifest{
		{
			Name:    "sleep",
			Command: []string{"sleep", "1"},
			Image:   "debian:latest",
			PostCommands: [][]string{
				{"echo", "from post-command"},
				{"head", "-1", "/duct.go"},
			},
			BindMounts: map[string]string{
				"duct.go": "/duct.go",
			},
		},
	}, "duct-test-network")

	if err := c.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := c.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}

	c = New(Manifest{
		{
			Name:     "early-terminator",
			Command:  []string{"sleep", "1"},
			Image:    "debian:latest",
			BootWait: 2 * time.Second,
			PostCommands: [][]string{
				{"echo", "from post-command"},
			},
		},
	}, "duct-test-network")

	if err := c.Launch(context.Background()); err == nil {
		t.Fatal("launch succeeded; should not have")
	}

	if err := c.Teardown(context.Background()); err == nil {
		t.Fatal("teardown did not fail with an error")
	}

	// start it a second time to make sure it was cleaned up, this time it will
	// succeed to run
	c = New(Manifest{
		{
			Name:     "early-terminator",
			Command:  []string{"sleep", "3"},
			Image:    "debian:latest",
			BootWait: 2 * time.Second,
			PostCommands: [][]string{
				{"echo", "from post-command"},
			},
		},
	}, "duct-test-network")

	if err := c.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := c.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}

	c = New(Manifest{
		{
			Name:         "post-command-exit",
			Command:      []string{"sleep", "infinity"},
			Image:        "debian:latest",
			PostCommands: [][]string{{"false"}},
		},
	}, "duct-test-network")

	t.Cleanup(func() {
		c.Teardown(context.Background())
	})

	if err := c.Launch(context.Background()); err == nil {
		t.Fatal("did not error running bad postcommand")
	}
}

func TestNetwork(t *testing.T) {
	b := Builder{
		"nc": {
			Dockerfile: "testdata/Dockerfile.nc",
			Context:    ".",
		},
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	c := New(Manifest{
		{
			Name:       "target",
			Command:    []string{"nc", "-k", "-l", "-p", "6000"},
			Image:      "nc",
			LocalImage: true,
			PortForwards: map[int]int{
				6000: 6000,
			},
		},
		{
			Name:         "pinger",
			Command:      []string{"sleep", "infinity"},
			PostCommands: [][]string{{"ping", "-c", "1", "target"}},
			Image:        "debian:latest",
		},
	}, "duct-test-network")

	t.Cleanup(func() {
		if err := c.Teardown(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	if err := c.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", "localhost:6000")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestAliveFunc(t *testing.T) {
	c := New(Manifest{
		{
			Name:  "target",
			Image: "nginx:latest",
			AliveFunc: func(ctx context.Context, client *dc.Client, id string) error {
				for {
					conn, err := net.Dial("tcp", "localhost:6000")
					if err != nil {
						log.Printf("Error while dialing container: %v", err)
						time.Sleep(100 * time.Millisecond)
						continue
					}
					conn.Close()
					return nil
				}
			},
			PortForwards: map[int]int{
				6000: 80,
			},
		},
		{
			Name:    "target-no-port",
			Command: []string{"sleep", "infinity"},
			Image:   "debian:latest",
			AliveFunc: func(ctx context.Context, client *dc.Client, id string) error {
				for {
					container, err := client.InspectContainer(id)
					if err != nil {
						log.Printf("Error inspecting container: %v", err)
						continue
					}

					if container.State.Running {
						return nil
					}

					log.Printf("Container %v is not running yet", id)
				}
			},
		},
	}, "duct-test-network")

	t.Cleanup(func() {
		if err := c.Teardown(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	if err := c.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSignals(t *testing.T) {
	c := New(Manifest{
		{
			Name:  "target",
			Image: "nginx:latest",
		},
	}, "duct-test-network")

	c.HandleSignals(false)

	if err := c.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}

	unix.Kill(os.Getpid(), unix.SIGINT)

	time.Sleep(time.Second)

	if err := c.Teardown(context.Background()); err == nil {
		t.Fatal("signal handling didn't work")
	}
}
