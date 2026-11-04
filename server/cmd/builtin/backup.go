package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// backupCmd backs the server up: it answers at once, and once the backup
// is done, through the output a source is sent later (cmd.Deferrer).
type backupCmd struct {
	owner
	srv serverAdapter
}

// backupListCmd lists the backups of the backup folder, newest first.
type backupListCmd struct {
	owner
	srv  serverAdapter
	List cmd.SubCommand `cmd:"list"`
}

// backupStatusCmd tells whether a backup runs, the last, and the next.
type backupStatusCmd struct {
	owner
	srv    serverAdapter
	Status cmd.SubCommand `cmd:"status"`
}

// backupVerifyCmd verifies a backup, by its record or in full.
type backupVerifyCmd struct {
	owner
	srv    serverAdapter
	Verify cmd.SubCommand               `cmd:"verify"`
	Name   string                       `cmd:"name"`
	Full   cmd.Optional[cmd.SubCommand] `cmd:"full"`
}

// saveHoldCmd, saveQueryCmd and saveResumeCmd are BDS's save hold, save
// query and save resume, for the panels that back a server up through its
// console. The files to copy are those of a copy of the world in the hold
// folder, not those of the world folder.
type (
	saveHoldCmd struct {
		owner
		srv  serverAdapter
		Hold cmd.SubCommand `cmd:"hold"`
	}
	saveQueryCmd struct {
		owner
		srv   serverAdapter
		Query cmd.SubCommand `cmd:"query"`
	}
	saveResumeCmd struct {
		owner
		srv    serverAdapter
		Resume cmd.SubCommand `cmd:"resume"`
	}
)

func newBackupCommand(srv serverAdapter) cmd.Command {
	return newCommand("backup", nil, backupCmd{srv: srv}, backupListCmd{srv: srv}, backupStatusCmd{srv: srv},
		backupVerifyCmd{srv: srv})
}

func newSaveCommand(srv serverAdapter) cmd.Command {
	return newCommand("save", nil, saveHoldCmd{srv: srv}, saveQueryCmd{srv: srv}, saveResumeCmd{srv: srv})
}

// actorOf returns who src is, as a backup records it.
func actorOf(srv serverAdapter, src cmd.Source) server.Actor {
	a := srv.ActorOf(src)
	return server.Actor{Kind: server.ActorKind(a.Kind), Name: a.String()}
}

// Run starts the backup, off the transaction of the command, and tells the
// source how it ended once it did.
func (c backupCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("Backing up...")
	send, by := outputLater(src), actorOf(c.srv, src)
	go func() {
		info, err := c.srv.Backup(context.Background(), by, "manual")
		out := &cmd.Output{}
		if err != nil {
			out.Errorf("Backup failed: %v", err)
		} else {
			out.Printf("Backup %s done: %s.", info.Name, backupSummary(info))
		}
		send(nil, out)
	}()
}

// backupSummary describes a backup in one line: its size, its files, how
// long it took, its checkpoint and how it was verified.
func backupSummary(info server.BackupInfo) string {
	s := fmt.Sprintf("%s, %d files", formatSize(info.Bytes), info.Files)
	if info.Linked > 0 {
		s += fmt.Sprintf(" (%d linked)", info.Linked)
	}
	if info.Took > 0 {
		s += fmt.Sprintf(", %.2f s", info.Took.Seconds())
	}
	if info.Checkpoint.ID != 0 {
		s += fmt.Sprintf(", checkpoint %d", info.Checkpoint.ID)
	}
	if info.Verified == "off" || info.Verified == "" {
		return s + ", not verified"
	}
	return s + ", verified (" + info.Verified + ")"
}

// formatSize returns n bytes as KiB, MiB or GiB, to one decimal.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 3 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %ciB", v, "KMGT"[i])
}

func (c backupListCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	list, err := c.srv.Backups()
	if err != nil {
		o.Error(err)
		return
	}
	if len(list) == 0 {
		o.Print("There are no backups")
		return
	}
	o.Printf("There are %d backup(s):", len(list))
	for _, info := range list {
		o.Printf("%s: %s by %s, %s, %s", info.Name, info.Created.Local().Format(time.DateTime), info.By, info.Format, backupSummary(info))
	}
}

func (c backupStatusCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	s := c.srv.BackupStatus()
	if s.Running {
		o.Printf("A backup is running (started %s by %s).", s.Started.Local().Format(time.TimeOnly), s.By)
	} else {
		o.Print("No backup is running.")
	}
	if s.Last != nil {
		o.Printf("The last backup is %s: %s.", s.Last.Name, backupSummary(*s.Last))
	}
	if s.LastErr != nil {
		o.Printf("The backup of %s failed: %v", s.LastErrAt.Local().Format(time.DateTime), s.LastErr)
	}
	if s.Next.IsZero() {
		o.Print("No backup is scheduled.")
	} else {
		o.Printf("The next backup is due at %s.", s.Next.Local().Format(time.DateTime))
	}
}

// Run verifies the backup off the transaction of the command, which a full
// verification, reading every key, would hold up.
func (c backupVerifyCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	_, full := c.Full.Load()
	how := "record"
	if full {
		how = "full"
	}
	o.Printf("Verifying %s (%s)...", c.Name, how)
	send := outputLater(src)
	go func() {
		info, err := c.srv.VerifyBackup(c.Name, full)
		out := &cmd.Output{}
		switch {
		case err != nil:
			out.Error(err)
		case info.Checkpoint.ID != 0:
			out.Printf("Backup %s is whole (%s): it holds checkpoint %d.", info.Name, how, info.Checkpoint.ID)
		default:
			out.Printf("Backup %s is whole (%s): no checkpoint is recorded of it.", info.Name, how)
		}
		send(nil, out)
	}()
}

// Run starts the copy of the world, and answers at once, as BDS's does.
func (c saveHoldCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := c.srv.HoldSave(actorOf(c.srv, src)); err != nil {
		o.Error(err)
		return
	}
	o.Print("Saving...")
}

// Run tells whether the files of the save held are ready, and lists them,
// as BDS's save query does: "Data saved. Files are now ready to be
// copied.", then "path:length, ..." in one line, the paths relative to the
// hold folder, which the line before names.
func (c saveQueryCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	h := c.srv.QuerySave()
	switch {
	case !h.Held:
		o.Error("No save is held: /save hold prepares one.")
	case h.Err != nil:
		o.Errorf("Saving failed: %v", h.Err)
	case !h.Ready:
		o.Print("A previous save has not been completed.")
	default:
		files := make([]string, len(h.Files))
		for i, f := range h.Files {
			files[i] = fmt.Sprintf("%s:%d", f.Path, f.Size)
		}
		o.Printf("The files to copy are in %s, a copy of the world that does not change.", h.Folder)
		o.Print("Data saved. Files are now ready to be copied.")
		o.Print(strings.Join(files, ", "))
	}
}

// Run deletes the files of the save held, off the transaction of the
// command, which a copy being prepared would hold up as it stops.
func (c saveResumeCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	send := outputLater(src)
	go func() {
		out := &cmd.Output{}
		if err := c.srv.ResumeSave(); err != nil {
			out.Error(err)
		} else {
			out.Print("Changes to the world are resumed.")
		}
		send(nil, out)
	}()
}
