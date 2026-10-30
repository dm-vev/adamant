"""Hardware execution tracing for a Reference-owned Windows BDS child only.

No instruction bytes or on-disk executable are changed. Thread debug registers
are restored before detaching. This is debugger-observed evidence, distinct
from the ordinary uninstrumented Script API captures.
"""
import ctypes as C
from ctypes import wintypes as W
import struct
import threading


class OwnedHardwareTrace:
    def __init__(self, process, memory, core_pointer, core_rva, chunk_rva, native, moving_rva=None, area_rva=None, ejection_rva=None, item_tick_rva=None, block_event_rva=None, bell_ring_rva=None):
        if process.poll() is not None:
            raise RuntimeError('owned reference process is not running')
        self.process, self.memory, self.core_pointer, self.native = process, memory, core_pointer, native
        self.core_address, self.chunk_address = memory.base + core_rva, memory.base + chunk_rva
        self.moving_address = memory.base + moving_rva if moving_rva is not None else None
        self.area_address = memory.base + area_rva if area_rva is not None else None
        self.ejection_address = memory.base + ejection_rva if ejection_rva is not None else None
        self.item_tick_address = memory.base + item_tick_rva if item_tick_rva is not None else None
        self.block_event_address = memory.base + block_event_rva if block_event_rva is not None else None
        self.bell_ring_address = memory.base + bell_ring_rva if bell_ring_rva is not None else None
        if sum(x is not None for x in (self.moving_address,self.area_address,self.ejection_address,self.block_event_address,self.bell_ring_address))>1:raise ValueError('DR3 has only one supplementary target')
        self.extra_address = self.moving_address or self.area_address or self.ejection_address or self.block_event_address or self.bell_ring_address
        self.kernel = C.WinDLL('kernel32', use_last_error=True)
        self.kernel.DebugActiveProcess.argtypes = [W.DWORD]
        self.kernel.DebugActiveProcessStop.argtypes = [W.DWORD]
        self.kernel.DebugSetProcessKillOnExit.argtypes = [W.BOOL]
        self.kernel.WaitForDebugEvent.argtypes = [C.c_void_p, W.DWORD]
        self.kernel.ContinueDebugEvent.argtypes = [W.DWORD, W.DWORD, W.DWORD]
        self.kernel.OpenThread.argtypes = [W.DWORD, W.BOOL, W.DWORD]
        self.kernel.OpenThread.restype = W.HANDLE
        self.kernel.GetThreadContext.argtypes = [W.HANDLE, C.c_void_p]
        self.kernel.SetThreadContext.argtypes = [W.HANDLE, C.c_void_p]
        self.kernel.CloseHandle.argtypes = [W.HANDLE]
        self.handles, self.original_debug = {}, {}
        self.owner_thread, self.active, self.chunks, self.outside = None, None, [], []
        self.areas = []
        self.ejection_active,self.ejections=None,[]
        self.item_ticks=[]
        self.block_events=[]
        self.bell_rings=[]
        self.lock, self.ready, self.stopping = threading.Lock(), threading.Event(), threading.Event()
        self.error, self.total_hits = None, 0
        self.thread = threading.Thread(target=self.run, daemon=True)

    def context(self, tid):
        if tid not in self.handles:
            handle = self.kernel.OpenThread(0x58, False, tid)
            if not handle:
                raise C.WinError(C.get_last_error())
            self.handles[tid] = handle
        buffer = C.create_string_buffer(0x4D0 + 16)
        pointer = (C.addressof(buffer) + 15) & ~15
        C.c_uint32.from_address(pointer + 0x30).value = 0x100013
        if not self.kernel.GetThreadContext(self.handles[tid], pointer):
            raise C.WinError(C.get_last_error())
        if tid not in self.original_debug:
            original = C.string_at(pointer + 0x48, 48)
            if struct.unpack_from('<Q', original, 40)[0] & 0xFF:
                raise RuntimeError('owned process thread already has hardware breakpoints')
            self.original_debug[tid] = original
        return buffer, pointer

    def set_context(self, tid, pointer):
        if not self.kernel.SetThreadContext(self.handles[tid], pointer):
            raise C.WinError(C.get_last_error())

    def arm(self, tid):
        buffer, pointer = self.context(tid)
        if self.owner_thread is not None and tid != self.owner_thread:
            return
        C.c_uint64.from_address(pointer + 0x50).value = self.chunk_address # DR1
        C.c_uint64.from_address(pointer + 0x70).value = 4 # local DR1, execute, length 1
        self.set_context(tid, pointer)

    def restore(self):
        for tid in list(self.handles):
            try:
                buffer, pointer = self.context(tid)
                C.memmove(pointer + 0x48, self.original_debug[tid], 48)
                self.set_context(tid, pointer)
            except OSError:
                pass # an EXIT_THREAD event may already have removed this thread

    def single_step(self, tid):
        buffer, pointer = self.context(tid)
        rip = C.c_uint64.from_address(pointer + 0xF8).value
        flags = C.c_uint64.from_address(pointer + 0x68).value # DR6
        rcx = C.c_uint64.from_address(pointer + 0x80).value
        rsp = C.c_uint64.from_address(pointer + 0x98).value
        if not flags & 15:
            return False
        self.total_hits += 1
        if flags & 2 and rip == self.item_tick_address:
            vector=struct.unpack('<Q',self.memory.read(rcx+0x218,8))[0]
            self.item_ticks.append(dict(actor=hex(rcx),position=list(struct.unpack('<3f',self.memory.read(vector,12))),
                velocity=list(struct.unpack('<3f',self.memory.read(vector+0x18,12))),
                pickup_delay=struct.unpack('<i',self.memory.read(rcx+0x44c,4))[0],sequence=self.total_hits))
        elif flags & 2 and rip == self.chunk_address:
            if self.owner_thread is None:
                self.owner_thread = tid
                for other in list(self.handles):
                    if other != tid:
                        try:
                            old_buffer, old = self.context(other)
                            C.memmove(old + 0x48, self.original_debug[other], 48)
                            self.set_context(other, old)
                        except OSError:
                            pass # a thread may have exited before its event is delivered
            if tid != self.owner_thread:
                raise RuntimeError('unexpected actor pass on another thread')
            position = list(struct.unpack('<ii', self.memory.read(rcx + 0x78, 8)))
            if any(abs(v) > 1000000 for v in position):
                raise RuntimeError(f'unexpected native chunk position {position}')
            return_address = struct.unpack('<Q', self.memory.read(rsp, 8))[0]
            with self.lock:
                if self.active is not None:
                    raise RuntimeError('nested chunk actor pass')
                self.active = dict(position=position, entry_sequence=self.total_hits,
                                   entry_caller=hex(return_address-self.memory.base),
                                   snow_random=struct.unpack('<I', self.memory.read(rcx + 0x1528, 4))[0], words=[])
            C.c_uint64.from_address(pointer + 0x48).value = self.core_address # DR0
            C.c_uint64.from_address(pointer + 0x58).value = return_address # DR2
            if self.extra_address is not None:C.c_uint64.from_address(pointer + 0x60).value = self.extra_address
            C.c_uint64.from_address(pointer + 0x70).value = 0x15 | (0x40 if self.extra_address is not None else 0)
            self.ready.set()
        elif flags & 1 and rip == self.core_address:
            if rcx == self.core_pointer:
                raw = self.memory.read(rcx, 0x9D4)
                self.native.reset(raw)
                value = self.native.draw()
                caller = struct.unpack('<Q', self.memory.read(rsp, 8))[0] - self.memory.base
                record = dict(value=value, caller=hex(caller), index=struct.unpack_from('<i', raw, 0x9CC)[0], sequence=self.total_hits)
                if caller==getattr(self,'random_float_caller',None):
                    record['float_caller']=hex(struct.unpack('<Q',self.memory.read(rsp+0x30,8))[0]-self.memory.base)
                if self.ejection_active is not None:
                    record['ejection_launch']=self.ejection_active['launch']
                    record['ejection_facing']=self.ejection_active['facing']
                if caller in getattr(self,'ejection_callers',set()):
                    frame=C.c_uint64.from_address(pointer+0xA0).value
                    record['ejection_origin']=list(struct.unpack('<3f',self.memory.read(frame+0x58,12)))
                if caller == 0x874129:
                    # Validated inspected BDS Random::nextInt(bound): RSI is
                    # the divisor; push RSI/sub RSP,0x20 puts its caller at
                    # Core-entry RSP+0x30. Keep this native context rather
                    # than guessing which bounded draw a raw word belongs to.
                    record['bound'] = C.c_uint64.from_address(pointer + 0xA8).value & 0xFFFFFFFF
                    record['bounded_caller'] = hex(struct.unpack('<Q', self.memory.read(rsp + 0x30, 8))[0] - self.memory.base)
                    container_context=getattr(self,'container_callers',{}).get(int(record['bounded_caller'],16))
                    if container_context is not None:
                        register,offset=container_context
                        actor=C.c_uint64.from_address(pointer+register).value+offset
                        record['container_position']=list(struct.unpack('<iii',self.memory.read(actor,12)))
                with self.lock:
                    if self.active is None:self.outside.append(record)
                    else:self.active['words'].append(record)
        elif flags & 4:
            with self.lock:
                if self.ejection_active is not None:
                    if rip!=self.ejection_active['return_address']:raise RuntimeError('unexpected ejection return address')
                    self.ejection_active['exit_sequence']=self.total_hits
                    del self.ejection_active['return_address']
                    self.ejections.append(self.ejection_active);self.ejection_active=None
                else:
                    if self.active is None:raise RuntimeError('actor-pass return without entry')
                    self.active['exit_sequence'] = self.total_hits
                    self.chunks.append(self.active);self.active = None
            C.c_uint64.from_address(pointer + 0x58).value = 0
            if self.item_tick_address is not None:C.c_uint64.from_address(pointer+0x50).value=self.item_tick_address
            C.c_uint64.from_address(pointer + 0x70).value = 5 | (0x40 if self.extra_address is not None else 0)
        elif flags & 8 and rip == self.ejection_address:
            if self.active is not None or self.ejection_active is not None:raise RuntimeError('nested ejection/actor trace')
            position=C.c_uint64.from_address(pointer+0x88).value
            return_address=struct.unpack('<Q',self.memory.read(rsp,8))[0]
            self.ejection_active=dict(launch=list(struct.unpack('<3f',self.memory.read(position,12))),
                facing=C.c_uint64.from_address(pointer+0xB8).value&255,return_address=return_address,entry_sequence=self.total_hits)
            C.c_uint64.from_address(pointer+0x58).value=return_address
            C.c_uint64.from_address(pointer+0x70).value=0x55
        elif flags & 8 and rip == self.bell_ring_address:
            record=dict(position=list(struct.unpack('<iii',self.memory.read(rcx+8,12))),
                direction=C.c_uint64.from_address(pointer+0x88).value&255,
                ticks=struct.unpack('<I',self.memory.read(rcx+0x48,4))[0],
                ringing=bool(self.memory.read(rcx+0xf0,1)[0]),
                forced=bool(self.memory.read(rsp+0x28,1)[0]),
                actor_is_null=C.c_uint64.from_address(pointer+0xc0).value==0,
                sequence=self.total_hits,caller=hex(struct.unpack('<Q',self.memory.read(rsp,8))[0]-self.memory.base),
                active_chunk=self.active['position'] if self.active is not None else None)
            with self.lock:self.bell_rings.append(record)
        elif flags & 8 and rip == self.block_event_address:
            position=C.c_uint64.from_address(pointer+0x88).value
            record=dict(position=list(struct.unpack('<iii',self.memory.read(position,12))),
                event_type=C.c_int32.from_address(pointer+0xB8).value,
                event_data=C.c_int32.from_address(pointer+0xC0).value,
                sequence=self.total_hits,caller=hex(struct.unpack('<Q',self.memory.read(rsp,8))[0]-self.memory.base))
            with self.lock:self.block_events.append(record)
        elif flags & 8 and rip == self.area_address:
            tick_pointer = C.c_uint64.from_address(pointer + 0x88).value
            record = dict(area=hex(rcx), tick=struct.unpack('<Q',self.memory.read(tick_pointer,8))[0],
                          randomize=bool(C.c_uint64.from_address(pointer + 0xB8).value & 0xFF),
                          caller=hex(struct.unpack('<Q',self.memory.read(rsp,8))[0]-self.memory.base),sequence=self.total_hits)
            # TickingArea::getView returns this+0x238. Its view's source
            # pointer at +8 is a child ChunkViewSource, distinct from the
            # area's embedded parent loading source at +0x78.
            view_source=struct.unpack('<Q',self.memory.read(rcx+0x240,8))[0]
            record['bounds']=list(struct.unpack('<12i',self.memory.read(view_source+0x140,48)))
            record['is_circle']=bool(self.memory.read(view_source+0x1A0,1)[0])
            with self.lock:self.areas.append(record)
        elif flags & 8 and rip == self.moving_address:
            record = dict(position=list(struct.unpack('<iii', self.memory.read(rcx - 0x38, 12))),
                          age=struct.unpack('<I', self.memory.read(rcx + 8, 4))[0],
                          flags=self.memory.read(rcx + 12, 1)[0])
            with self.lock:
                if self.active is None:raise RuntimeError('moving actor tick outside native chunk pass')
                self.active.setdefault('moving', []).append(record)
        else:
            return False
        # Resume flag skips an execution breakpoint for the current instruction;
        # no instruction replacement or single-step instruction patch is used.
        C.c_uint32.from_address(pointer + 0x44).value |= 0x10000
        C.c_uint64.from_address(pointer + 0x68).value = 0
        self.set_context(tid, pointer)
        return True

    def run(self):
        attached = False
        event = C.create_string_buffer(176)
        try:
            if not self.kernel.DebugActiveProcess(self.process.pid):
                raise C.WinError(C.get_last_error())
            attached = True
            self.kernel.DebugSetProcessKillOnExit(False)
            while True:
                if not self.kernel.WaitForDebugEvent(event, 100):
                    code = C.get_last_error()
                    if code not in (0, 121, 258):raise C.WinError(code)
                    if self.process.poll() is not None:break
                    continue
                kind, pid, tid = struct.unpack_from('<III', event)
                status, stop = 0x10002, self.stopping.is_set()
                try:
                    if stop:self.restore()
                    elif kind in (2, 3): # CREATE_THREAD / CREATE_PROCESS
                        self.arm(tid)
                    elif kind == 4: # EXIT_THREAD
                        handle = self.handles.pop(tid, None)
                        if handle:self.kernel.CloseHandle(handle)
                        self.original_debug.pop(tid, None)
                    elif kind == 1:
                        exception = struct.unpack_from('<I', event, 16)[0]
                        if exception == 0x80000004:
                            if not self.single_step(tid):status = 0x80010001
                        elif exception != 0x80000003:
                            status = 0x80010001
                    if kind in (3, 6): # close the debugger's image file handles
                        file_handle = struct.unpack_from('<Q', event, 16)[0]
                        if file_handle:self.kernel.CloseHandle(file_handle)
                except Exception:
                    self.restore()
                    raise
                finally:
                    if not self.kernel.ContinueDebugEvent(pid, tid, status):raise C.WinError(C.get_last_error())
                if kind == 5:
                    attached = False;break
                if stop:break
        except Exception as error:
            self.error = error;self.ready.set()
        finally:
            if attached:self.kernel.DebugActiveProcessStop(self.process.pid)
            for handle in self.handles.values():self.kernel.CloseHandle(handle)

    def start(self):
        self.thread.start()
        if not self.ready.wait(20):raise RuntimeError('native actor breakpoint was not reached')
        if self.error:raise self.error

    def checkpoint(self):
        if self.error:raise self.error
        with self.lock:
            if self.active is not None or self.ejection_active is not None:raise RuntimeError('Script checkpoint is inside an actor/ejection pass')
            result = dict(chunks=self.chunks, outside=self.outside, areas=self.areas,ejections=self.ejections,item_post_ticks=self.item_ticks,block_events=self.block_events,bell_rings=self.bell_rings)
            self.chunks, self.outside, self.areas = [], [], []
            self.ejections=[]
            self.item_ticks=[]
            self.block_events=[]
            self.bell_rings=[]
            return result

    def stop(self):
        self.stopping.set();self.thread.join(timeout=20)
        if self.thread.is_alive():raise RuntimeError('native debugger did not detach')
        if self.error:raise self.error
