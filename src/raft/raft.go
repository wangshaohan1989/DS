package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"labgob"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"
)
import "labrpc"

// import "bytes"
// import "labgob"

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type nodeTye string

const (
	Candidate nodeTye = "candidate"
	Follower          = "follower"
	Leader            = "leader"
)

type SnapShot struct {
	LastIncludedIndex int
	LastIncludedTerm  int
	State             map[string]string
	SerialNums        map[int64]int
	ConfigNum int
}

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
	CommandTerm  int
	Snpst        SnapShot
}

type Log struct {
	Term    int
	Command interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu                sync.Mutex          // Lock to protect shared access to this peer's state
	peers             []*labrpc.ClientEnd // RPC end points of all peers
	persister         *Persister          // Object to hold this peer's persisted state
	me                int                 // this peer's index into peers[]
	lastHeardTime     int64               //the peer heard from leader last time
	electionTimeOut   int64
	heartBeatInterval int
	applyCh           chan ApplyMsg

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	CurrentTerm int
	VotedFor    int
	Logs        []Log

	//volatile state on all servers
	CommitIndex int
	LastApplied int
	State       nodeTye

	//volatile state on leaders
	NextIndex  []int
	MatchIndex []int

	LastIncludedIndex int
	LastIncludedTerm  int

	//use to debug
	RfLog   *log.Logger
	isAlive bool
	logFile *os.File
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	rf.mu.Lock()
	term = rf.CurrentTerm
	switch rf.State {
	case Leader:
		isleader = true
	default:
		isleader = false
	}
	rf.mu.Unlock()
	// Your code here (2A).
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.CurrentTerm)
	e.Encode(rf.VotedFor)
	e.Encode(rf.Logs)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)

	//fmt.Printf("peer %d persist:" +
	//	"logs: %v\n" +
	//	"voted for: %d\n" +
	//	"current term: %d\n\n",
	//	rf.me,rf.Logs,rf.VotedFor,rf.CurrentTerm)

}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var logs []Log
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil || d.Decode(&logs) != nil {
		fmt.Println("decode error!")
	} else {
		rf.CurrentTerm = currentTerm
		rf.VotedFor = votedFor
		rf.Logs = logs
	}
	//fmt.Printf("peer %d read persist:" +
	//	"logs: %v\n" +
	//	"voted for: %d\n" +
	//	"current term: %d\n\n",
	//	rf.me,rf.Logs,rf.VotedFor,rf.CurrentTerm)

}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term         int
	ReceivedTerm int
	VoterGranted bool
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	reply.ReceivedTerm = args.Term
	reply.Term = rf.CurrentTerm
	if args.Term < rf.CurrentTerm {
		reply.VoterGranted = false
	}
	if args.Term == rf.CurrentTerm {
		//在follower的同一个term中，给定candidate的term，最多只能为一个candidate投票
		if rf.VotedFor == -1 || rf.VotedFor == args.CandidateId {
			if args.LastLogTerm > rf.Logs[len(rf.Logs)-1].Term || (args.LastLogTerm == rf.Logs[len(rf.Logs)-1].Term && args.LastLogIndex >= len(rf.Logs)-1+rf.LastIncludedIndex) {
				reply.VoterGranted = true
				rf.VotedFor = args.CandidateId
			}
		}
	}
	if args.Term > rf.CurrentTerm {
		if args.LastLogTerm > rf.Logs[len(rf.Logs)-1].Term || (args.LastLogTerm == rf.Logs[len(rf.Logs)-1].Term && args.LastLogIndex >= len(rf.Logs)-1+rf.LastIncludedIndex) {
			reply.VoterGranted = true
			rf.VotedFor = args.CandidateId
		}
		rf.CurrentTerm = args.Term
		oldState := rf.State
		rf.State = Follower
		if oldState == Leader {
			rf.resetElectionTimeOut()
			go rf.startElectionTimeOut()
		}
		rf.RfLog.Println("becomes follower in RequestVote")
	}
	rf.persist()
	rf.RfLog.Printf("receive RequestVoteArgs: %v, Reply: %v\n", args, reply)
	if reply.VoterGranted {
		rf.resetElectionTimeOut()
	}
	rf.mu.Unlock()

	return

}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) startElection() {
	rf.RfLog.Println("start a new election")
	//	fmt.Printf("%s peer %d starts election\n", time.Now().Format("2006/01/02/ 15:03:04.000"), rf.me)
	rf.mu.Lock()
	rf.CurrentTerm += 1
	rf.persist()
	rf.VotedFor = rf.me
	rva := RequestVoteArgs{}
	rva.Term = rf.CurrentTerm
	rva.LastLogIndex = len(rf.Logs) - 1 + rf.LastIncludedIndex
	rva.LastLogTerm = rf.Logs[len(rf.Logs)-1].Term
	rva.CandidateId = rf.me
	rf.resetElectionTimeOut()
	rf.mu.Unlock()
	go rf.startElectionTimeOut()

	//计算得票数
	count := 1

	//fmt.Println("start")
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		go func(peer int) {
			rvr := RequestVoteReply{}
			rf.RfLog.Printf("send RequestVoteArgs: %v to peer: %d\n", rva, peer)
			ok := rf.sendRequestVote(peer, &rva, &rvr)
			if ok {
				rf.RfLog.Printf("request vote receive reply from peer %d, reply: %v, args: %v\n", peer, rvr, rva)
				rf.mu.Lock()
				if rvr.Term > rf.CurrentTerm {
					rf.CurrentTerm = rvr.Term
					rf.State = Follower
					rf.RfLog.Println("becomes follower in election")
					rf.persist()
					rf.resetElectionTimeOut()
					rf.mu.Unlock()
					go rf.startElectionTimeOut()
					return
				}
				rf.mu.Unlock()
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if rvr.VoterGranted && rvr.ReceivedTerm == rf.CurrentTerm {
					count += 1
					if count > len(rf.peers)/2 {
						rf.RfLog.Println("becomes leader in election")
						//fmt.Printf("%s peer %d becomes leader\n", time.Now().Format("2006/01/02/ 15:03:04.000"), rf.me)
						rf.State = Leader
						//这个地方当初没写也允许通过了TestBasicAgree2B 和 TestFailAgree2B，值得思考
						for j := 0; j < len(rf.NextIndex); j++ {
							rf.MatchIndex[j] = 0
							rf.NextIndex[j] = len(rf.Logs) + rf.LastIncludedIndex
						}
						go rf.startHeartBeat()
						go rf.checkLeaderCommitIndex()
						go rf.replicateLog()
						//如果不加这一行，则选出的leader会启动多个心跳线程
						count = 0
					}
				}
			}

		}(i)
	}

}

func (rf *Raft) startElectionTimeOut() {
	var timeOut int64
	var constantItv int64
	timeOut = 0
	constantItv = 10

	for true {
		time.Sleep(time.Duration(constantItv) * time.Millisecond)
		rf.mu.Lock()
		timeOut = time.Now().UnixNano()/int64(time.Millisecond) - rf.lastHeardTime
		if timeOut >= rf.electionTimeOut {
			rf.mu.Unlock()
			break
		}
		rf.mu.Unlock()
	}
	rf.mu.Lock()
	//只有Follower和Candidate在election time out后才会发起投票，由于本程序的原因，在candidate选举时也会启动
	//一个electionTimeOut线程，其在成为Leader不应该再次进行投票了
	if rf.State == Follower || rf.State == Candidate {
		rf.State = Candidate
		rf.mu.Unlock()
		rf.startElection()
		return
	}
	rf.mu.Unlock()

}
func (rf *Raft) resetElectionTimeOut() {

	rand.Seed(time.Now().UnixNano())
	//fmt.Printf("peer %d reset timeout1\n",rf.me)
	rf.lastHeardTime = time.Now().UnixNano() / int64(time.Millisecond)
	rf.electionTimeOut = rand.Int63n(300) + 500
	//fmt.Printf("peer %d reset timeout2\n",rf.me)
}

type AppendEntryArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Log
	LeaderCommit int
}

type AppendEntryReply struct {
	Term         int
	ReceivedTerm int
	Success      bool
	SnpLPrev     bool
}

func (rf *Raft) AppendEntries(args *AppendEntryArgs, reply *AppendEntryReply) {
	//rf.RfLog.Printf("receive AppendEnryArgs: %v\n", args)
	//defer rf.RfLog.Printf("AppendEntries reply: %v, args:%v\n", reply, args)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term >= rf.CurrentTerm {
		rf.resetElectionTimeOut()
	}

	if args.Term > rf.CurrentTerm {
		rf.RfLog.Println("becomes follower in AppendEntries")
		rf.CurrentTerm = args.Term
		//need to fix
		oldState := rf.State
		rf.State = Follower
		if oldState == Leader {
			go rf.startElectionTimeOut()
		}
		rf.persist()
	}
	reply.ReceivedTerm = args.Term
	reply.Term = rf.CurrentTerm
	//args.PrevLogIndex>len(rf.Logs)-1这种情况也算不match
	//rf.RfLog.Printf("AppendEntries,args.PreLogIndedx: %d, rf.LastIncludedIndex: %d, true Index:%d, logs: %v\n", args.PrevLogIndex, rf.LastIncludedIndex, args.PrevLogIndex-rf.LastIncludedIndex, rf.Logs)
	if args.PrevLogIndex < rf.LastIncludedIndex {
		reply.SnpLPrev = true
		return
	}
	if args.Term < rf.CurrentTerm || args.PrevLogIndex > len(rf.Logs)-1+rf.LastIncludedIndex || rf.Logs[args.PrevLogIndex-rf.LastIncludedIndex].Term != args.PrevLogTerm {
		reply.Success = false
		return
	}
	if rf.State == Candidate {
		rf.State = Follower
	}
	reply.Success = true
	//以startPosition为起点,dist是插入点appendPosition到startPosition的距离
	startPosition := args.PrevLogIndex + 1
	dist := 0
	for ; dist < len(args.Entries) && (startPosition+dist) < len(rf.Logs)+rf.LastIncludedIndex; dist++ {
		if rf.Logs[startPosition+dist-rf.LastIncludedIndex].Term != args.Entries[dist].Term {
			rf.Logs = rf.Logs[:startPosition+dist-rf.LastIncludedIndex]
			break
		}
	}
	if len(args.Entries) == 0 {
		//	fmt.Printf("%s leader %d send hb to peer %d\n ", time.Now().Format("2006/01/02/ 15:03:04.000"), args.LeaderId, rf.me)
	} else {
		//	fmt.Printf("%s leader %d replicate peer %d: %v\n ", time.Now().Format("2006/01/02/ 15:03:04.000"), args.LeaderId, rf.me, args.Entries[dist:])
	}
	rf.Logs = append(rf.Logs, args.Entries[dist:]...)
	rf.persist()
	//fmt.Printf("after: peer %d logs: %v\n",rf.me,rf.Logs)
	//fmt.Printf("before: leaderCommit: %d, peer %d Commit: %d, applied:%d\n",args.LeaderCommit,rf.me,rf.CommitIndex,rf.LastApplied)
	if args.LeaderCommit > rf.CommitIndex {
		if args.LeaderCommit < args.PrevLogIndex+dist {
			rf.CommitIndex = args.LeaderCommit
		} else {
			rf.CommitIndex = args.PrevLogIndex + dist
		}
	}
	//fmt.Printf("dist : %d, prev index: %d\n",dist,args.PrevLogIndex)
	//fmt.Printf("after: leaderCommit: %d, peer %d Commit: %d, applied: %d\n",args.LeaderCommit,rf.me,rf.CommitIndex,rf.LastApplied)

}
func (rf *Raft) sendAppendEntries(server int, args *AppendEntryArgs, reply *AppendEntryReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) startHeartBeat() {
	//fmt.Println("start hb")
	rf.heartBeatInterval = 200
	for true {
		for i := 0; i < len(rf.peers); i++ {
			rf.mu.Lock()
			if rf.State != Leader || rf.isAlive == false {
				//fmt.Printf("leader %d return !!!\n", rf.me)
				rf.mu.Unlock()
				return
			}
			rf.mu.Unlock()
			if i != rf.me {
				go func(peer int) {
					reply := AppendEntryReply{}
					args := AppendEntryArgs{}
					rf.mu.Lock()
					if rf.State != Leader {
						rf.mu.Unlock()
						return
					}
					args.LeaderId = rf.me
					args.Term = rf.CurrentTerm
					nextIndex := rf.NextIndex[peer]
					if nextIndex <= rf.LastIncludedIndex {
						rf.RfLog.Printf("StartHeartBeat, peer %d lag, need to install snapshot\n", peer)
						snapshot := SnapShot{}
						rf.RfLog.Printf("start to read snapshot\n")
						data := rf.persister.ReadSnapshot()
						rf.RfLog.Printf("read snapshot succeed\n")
						r := bytes.NewBuffer(data)
						d := labgob.NewDecoder(r)
						var lastIncludeTerm int
						var lastIncludeIndex int
						var state map[string]string
						if d.Decode(&lastIncludeTerm) != nil ||
							d.Decode(&lastIncludeIndex) != nil || d.Decode(&state) != nil {
							rf.RfLog.Println("decode snapshot error")
						} else {
							snapshot.LastIncludedIndex = lastIncludeIndex
							snapshot.LastIncludedTerm = lastIncludeTerm
							snapshot.State = state
						}
						installSnapshot := InstallSnapshotArgs{Term: rf.CurrentTerm, LeaderId: rf.me, SnpSt: snapshot}
						rf.mu.Unlock()
						reply := InstallSnapshotReply{}
						rf.RfLog.Printf("StartHeartBeat, send to peer %d InstallSnapshotArgs: %v\n", peer, installSnapshot)
						ok := rf.sendInstallSnapshot(peer, &installSnapshot, &reply)
						rf.mu.Lock()
						if ok {
							rf.RfLog.Printf("StartHeartbeat, InstallSnapshot reply from peer %d: %v, args: %v\n", peer, reply, args)
							if reply.Term > rf.CurrentTerm {
								rf.RfLog.Println("becomes follower in StartHeartBeat.InstallSnapshot")
								rf.CurrentTerm = reply.Term
								rf.State = Follower
								rf.persist()
								rf.resetElectionTimeOut()
								rf.mu.Unlock()
								go rf.startElectionTimeOut()
								return
							}
							if reply.IsApplied {
								rf.NextIndex[peer] = snapshot.LastIncludedIndex + 1
								rf.MatchIndex[peer] = snapshot.LastIncludedIndex + 1
								rf.RfLog.Printf("next index: %v\n", rf.NextIndex)
							}
						}
						rf.mu.Unlock()
						return
					}
					args.PrevLogIndex = nextIndex - 1
					//rf.RfLog.Printf("StartHeartBeat,args.PreLogIndedx: %d, rf.LastIncludedIndex: %d, true Index:%d, rf.Logs: %v\n", args.PrevLogIndex, rf.LastIncludedIndex, args.PrevLogIndex-rf.LastIncludedIndex, rf.Logs)
					args.PrevLogTerm = rf.Logs[args.PrevLogIndex-rf.LastIncludedIndex].Term
					args.LeaderCommit = rf.CommitIndex
					rf.mu.Unlock()

					rf.sendAppendEntries(peer, &args, &reply)
					rf.mu.Lock()
					if reply.Term > rf.CurrentTerm {
						rf.CurrentTerm = reply.Term
						//rf.RfLog.Println("becomes follower in startHeartBeat")
						rf.State = Follower
						rf.persist()
						rf.resetElectionTimeOut()
						rf.mu.Unlock()
						go rf.startElectionTimeOut()
						return
					}
					rf.mu.Unlock()

				}(i)

			}
		}
		time.Sleep(time.Duration(rf.heartBeatInterval) * time.Millisecond)
	}
}

func (rf *Raft) replicateLog() {
	for true {
		rf.mu.Lock()
		peersNum := len(rf.peers)
		rf.mu.Unlock()
		for i := 0; i < peersNum; i++ {
			if i == rf.me {
				continue
			}
			go func(peer int) {
				rf.mu.Lock()
				if rf.State != Leader {
					rf.mu.Unlock()
					return
				}
				nextIndex := rf.NextIndex[peer]
				if nextIndex <= rf.LastIncludedIndex {
					rf.RfLog.Printf("Replicate, peer %d lag, need to install snapshot\n", peer)
					rf.RfLog.Printf("start to read snapshot\n")
					snapshot,_:=rf.GetSnapShot()
					installSnapshot := InstallSnapshotArgs{Term: rf.CurrentTerm, LeaderId: rf.me, SnpSt: snapshot}
					rf.mu.Unlock()
					reply := InstallSnapshotReply{}
					rf.RfLog.Printf("Replicate, send to peer %d InstallSnapshotArgs: %v\n", peer, installSnapshot)
					ok := rf.sendInstallSnapshot(peer, &installSnapshot, &reply)
					rf.mu.Lock()
					if ok {
						rf.RfLog.Printf("Replicate, InstallSnapshot reply from peer %d: %v, args: %v\n", peer, reply, installSnapshot)
						if reply.Term > rf.CurrentTerm {
							rf.RfLog.Println("becomes follower in Replicate.InstallSnapshot")
							rf.CurrentTerm = reply.Term
							rf.State = Follower
							rf.persist()
							rf.resetElectionTimeOut()
							rf.mu.Unlock()
							go rf.startElectionTimeOut()
							return
						}
						if reply.IsApplied {
							rf.NextIndex[peer] = snapshot.LastIncludedIndex + 1
							rf.MatchIndex[peer] = snapshot.LastIncludedIndex + 1
							rf.RfLog.Printf("next index: %v\n", rf.NextIndex)
						}
					}
					rf.mu.Unlock()
					return
				}
				isLarge := len(rf.Logs)-1+rf.LastIncludedIndex >= nextIndex
				if !isLarge {
					rf.mu.Unlock()
					return
				}
				args := AppendEntryArgs{}
				reply := AppendEntryReply{}
				args.Term = rf.CurrentTerm
				//rf.RfLog.Printf("Replicate, logs: %v, snapshot index/term: %d/%d, PrevLogIndex: %d, true index: %d\n", rf.Logs, rf.LastIncludedIndex, rf.LastIncludedTerm, nextIndex-1, nextIndex-1-rf.LastIncludedIndex)
				args.PrevLogIndex = nextIndex - 1
				args.PrevLogTerm = rf.Logs[args.PrevLogIndex-rf.LastIncludedIndex].Term

				args.LeaderCommit = rf.CommitIndex
				args.LeaderId = rf.me
				entries := rf.Logs[nextIndex-rf.LastIncludedIndex : nextIndex+1-rf.LastIncludedIndex]
				args.Entries = append(args.Entries, entries...)
				rf.mu.Unlock()
				//rf.RfLog.Printf("send to peer %d ReplicateArgs: %v\n", peer, args)
				ok := rf.sendAppendEntries(peer, &args, &reply)
				if ok {
					//rf.RfLog.Printf("Replicate reply from peer %d: %v, args: %v\n", peer, reply, args)
					rf.mu.Lock()
					if reply.ReceivedTerm != rf.CurrentTerm {
						rf.mu.Unlock()
						return
					}
					if reply.Term > rf.CurrentTerm {
						rf.CurrentTerm = reply.Term
						//rf.RfLog.Println("becomes follower in replicateLog")
						rf.State = Follower
						rf.persist()
						rf.resetElectionTimeOut()
						rf.mu.Unlock()
						go rf.startElectionTimeOut()
						return
					}
					if reply.SnpLPrev {
						rf.mu.Unlock()
						return
					}
					if reply.Success {
						rf.NextIndex[peer] = nextIndex + len(entries)
						rf.MatchIndex[peer] = args.PrevLogIndex + len(entries)
						//rf.RfLog.Printf("next index: %v\n", rf.NextIndex)
					} else {
						rf.NextIndex[peer] = nextIndex - 1
						//rf.RfLog.Printf("next index: %v\n", rf.NextIndex)
					}

					rf.mu.Unlock()
				}

			}(i)
			rf.mu.Lock()
			if rf.State != Leader || rf.isAlive == false {
				rf.mu.Unlock()
				return
			}
			rf.mu.Unlock()
		}

		time.Sleep(time.Duration(30) * time.Millisecond)
	}

}

type InstallSnapshotArgs struct {
	Term     int
	LeaderId int
	SnpSt    SnapShot
}
type InstallSnapshotReply struct {
	Term      int
	IsApplied bool
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.RfLog.Printf("receive InstallSnapshot: %v\n", args)
	defer rf.RfLog.Printf("InstallSnapshot reply: %v, args: %v\n", reply, args)
	rf.mu.Lock()
	reply.Term = rf.CurrentTerm
	if args.Term < rf.CurrentTerm {
		reply.IsApplied = false
		rf.mu.Unlock()
		return
	}
	rf.resetElectionTimeOut()
	if rf.CurrentTerm < args.Term {
		rf.CurrentTerm = args.Term
		oldState := rf.State
		rf.State = Follower
		if oldState == Leader {
			go rf.startElectionTimeOut()
		}
		rf.RfLog.Printf("becomes follower in InstallSnaoshot")
	}

	//if existing logs has same index and term as args.Snpst last include entry
	if rf.LastIncludedIndex < args.SnpSt.LastIncludedIndex && args.SnpSt.LastIncludedIndex <= rf.LastIncludedIndex+len(rf.Logs)-1 &&
		rf.Logs[args.SnpSt.LastIncludedIndex-rf.LastIncludedIndex].Term == args.SnpSt.LastIncludedTerm {
		rf.Logs[0].Term = rf.LastIncludedTerm
		rf.Logs = append(rf.Logs[0:1], rf.Logs[args.SnpSt.LastIncludedIndex+1-rf.LastIncludedIndex:]...)
		reply.IsApplied = false

	} else {
		rf.Logs[0].Term = rf.LastIncludedTerm
		rf.Logs = rf.Logs[0:1]
		rf.LastApplied = rf.LastIncludedIndex
		rf.CommitIndex = rf.LastIncludedIndex
		reply.IsApplied = true
	}
	rf.LastIncludedTerm = args.SnpSt.LastIncludedTerm
	rf.LastIncludedIndex = args.SnpSt.LastIncludedIndex
	applyMsg := ApplyMsg{CommandValid: false, Snpst: args.SnpSt}
	rf.persist()
	rf.mu.Unlock()
	//install snapshot
	rf.SaveSnapShot(args.SnpSt)

	go func() { rf.applyCh <- applyMsg }()

}
func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	rf.RfLog.Printf("send Installsnapshot RPC: %v\n", args)
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	rf.RfLog.Printf("Installsnapshot RPC reply: %v\n", reply)
	return ok
}

func (rf *Raft) checkApplied() {
	for true {

		rf.mu.Lock()
		if rf.isAlive == false {
			rf.mu.Unlock()
			break
		}
		if rf.CommitIndex > rf.LastApplied {
			rf.LastApplied += 1
			rf.RfLog.Printf("rf.LastApplied: %d, rf.LastIncludedIdnex: %d, true index: %d, rf.Logs: %v\n", rf.LastApplied, rf.LastIncludedIndex, rf.LastApplied-rf.LastIncludedIndex, rf.Logs)
			if rf.LastApplied > rf.LastIncludedIndex {
				applyMsg := ApplyMsg{Command: rf.Logs[rf.LastApplied-rf.LastIncludedIndex].Command, CommandIndex: rf.LastApplied, CommandValid: true, CommandTerm: rf.Logs[rf.LastApplied-rf.LastIncludedIndex].Term}
				go func() {
					rf.applyCh <- applyMsg
				}()
			}

		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(10) * time.Millisecond)
	}
}

func (rf *Raft) checkLeaderCommitIndex() {
	for true {
		rf.mu.Lock()
		if rf.State != Leader || rf.isAlive == false {
			rf.mu.Unlock()
			break
		}
		for N := rf.CommitIndex + 1; N < len(rf.Logs)+rf.LastIncludedIndex; N++ {
			count := 0
			if rf.Logs[N-rf.LastIncludedIndex].Term == rf.CurrentTerm {
				for p := 0; p < len(rf.MatchIndex); p++ {
					if rf.MatchIndex[p] >= N {
						count++
					}
				}
				if count > len(rf.MatchIndex)/2 {
					rf.CommitIndex = N
					break
				}
			}
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(10) * time.Millisecond)
	}
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true
	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.State != Leader {
		isLeader = false
	} else {
		index = len(rf.Logs) + rf.LastIncludedIndex

		rf.Logs = append(rf.Logs, Log{Term: rf.CurrentTerm, Command: command})
		rf.NextIndex[rf.me] = len(rf.Logs) + rf.LastIncludedIndex
		rf.MatchIndex[rf.me] = len(rf.Logs) + rf.LastIncludedIndex - 1
		rf.persist()
		term = rf.CurrentTerm
	}

	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
	rf.mu.Lock()
	rf.isAlive = false
	rf.mu.Unlock()
}

func (rf *Raft) GetStateSize() (size int) {
	return rf.persister.RaftStateSize()
}

func (rf *Raft) GetSnapShot() (SnapShot, bool) {

	snapshot := SnapShot{State: make(map[string]string), SerialNums: make(map[int64]int)}
	data := rf.persister.ReadSnapshot()
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var lastIncludeTerm int
	var lastIncludeIndex int
	var state map[string]string
	var serialNums map[int64]int
	var configNum int
	if d.Decode(&lastIncludeTerm) != nil ||
		d.Decode(&lastIncludeIndex) != nil || d.Decode(&state) != nil || d.Decode(&serialNums) != nil ||
		d.Decode(&configNum)!=nil{
		rf.RfLog.Println("decode snapshot error")

		return snapshot, false
	} else {
		snapshot.LastIncludedIndex = lastIncludeIndex
		snapshot.LastIncludedTerm = lastIncludeTerm
		snapshot.State = state
		snapshot.SerialNums = serialNums
		snapshot.ConfigNum=configNum
	}
	rf.RfLog.Printf("read snapshot: %v\n", snapshot)

	return snapshot, true
}

func (rf *Raft) SaveSnapShot(snapShot SnapShot) {

	//discard logs
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.RfLog.Printf("snap shot: %v, logs: %v\n", snapShot, rf.Logs)
	oldSnapshot, ok := rf.GetSnapShot()
	if ok && oldSnapshot.LastIncludedIndex >= snapShot.LastIncludedIndex {
		return
	}
	index := rf.LastIncludedIndex
	rf.LastIncludedIndex = snapShot.LastIncludedIndex
	rf.LastIncludedTerm = snapShot.LastIncludedTerm
	rf.Logs[0].Term = rf.LastIncludedTerm
	rf.Logs = append(rf.Logs[0:1], rf.Logs[snapShot.LastIncludedIndex+1-index:]...)

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	err1 := e.Encode(rf.CurrentTerm)
	err2 := e.Encode(rf.VotedFor)
	err3 := e.Encode(rf.Logs)
	if err1 != nil {
		panic(err1)
	}
	if err2 != nil {
		panic(err2)
	}
	if err3 != nil {
		panic(err3)
	}
	stateBytes := w.Bytes()

	w1 := new(bytes.Buffer)
	e1 := labgob.NewEncoder(w1)
	err1 = e1.Encode(snapShot.LastIncludedTerm)
	err2 = e1.Encode(snapShot.LastIncludedIndex)
	err3 = e1.Encode(snapShot.State)
	err4 := e1.Encode(snapShot.SerialNums)
	err5:=e1.Encode(snapShot.ConfigNum)
	if err1 != nil {
		panic(err1)
	}
	if err2 != nil {
		panic(err2)
	}
	if err3 != nil {
		panic(err3)
	}
	if err4 != nil {
		panic(err4)
	}
	if err5!=nil{
		panic(err5)
	}
	snapShotBytes := w1.Bytes()

	rf.persister.SaveStateAndSnapshot(stateBytes, snapShotBytes)

}

func (rf *Raft) GetLogs() []Log {
	rf.mu.Lock()
	logs := rf.Logs
	rf.mu.Unlock()
	return logs
}

func (rf *Raft) printState() {
	fmt.Printf(">>>>>>>>peer %d state start print\n"+
		"state: %s\n"+
		"current term: %d\n"+
		"voted for: %d\n"+
		"commit inex: %d\n"+
		"last applied: %d\n"+
		"logs: %v\n"+
		"next index: %v\n"+
		"match index: %v\n"+
		"<<<<<<<<<peer %d state end print\n\n",

		rf.me,
		rf.State,
		rf.CurrentTerm,
		rf.VotedFor,
		rf.CommitIndex,
		rf.LastApplied,
		rf.Logs,
		rf.NextIndex,
		rf.MatchIndex,
		rf.me)
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	// Your initialization code here (2A, 2B, 2C).
	//f, err := os.Create("raft" + strconv.Itoa(rf.me) + ".log")
	//rf.logFile = f
	//if err != nil {
	//	panic(err)
	//}
	rf.RfLog = log.New(ioutil.Discard, "[raft "+strconv.Itoa(rf.me)+"] ", log.Lmicroseconds)
	rf.CurrentTerm = 0
	rf.resetElectionTimeOut()
	rf.State = Follower
	rf.CommitIndex = 0
	rf.LastApplied = 0
	rf.VotedFor = -1
	rf.isAlive = true
	rf.applyCh = applyCh
	snapShot, readOk := rf.GetSnapShot()
	if readOk {
		rf.LastIncludedIndex = snapShot.LastIncludedIndex
		rf.LastIncludedTerm = snapShot.LastIncludedTerm
		rf.CommitIndex = snapShot.LastIncludedIndex
		rf.LastApplied = snapShot.LastIncludedIndex
	} else {
		rf.LastIncludedIndex = 0
		rf.LastIncludedTerm = 0
		rf.CommitIndex = 0
		rf.LastApplied = 0
	}

	rf.Logs = append(rf.Logs, Log{rf.LastIncludedTerm, 0})
	for j := 0; j < len(rf.peers); j++ {
		rf.NextIndex = append(rf.NextIndex, len(rf.Logs)+rf.LastIncludedIndex)
		rf.MatchIndex = append(rf.MatchIndex, 0)
	}
	//rf.persist()
	go rf.startElectionTimeOut()
	go rf.checkApplied()
	//fmt.Printf("last heard time %d, timeout %d\n", rf.lastHeardTime, rf.electionTimeOut)
	// initialize from state persisted before a crash
	rf.mu.Lock()
	rf.readPersist(persister.ReadRaftState())
	rf.mu.Unlock()

	return rf
}
