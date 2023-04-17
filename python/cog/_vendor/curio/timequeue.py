# timequeue.py
#
# A Discussion About Time.
#
# Internally, Curio must manage time for two different reasons,
# sleeping and for timeouts.  Aside from toy examples, most real-world
# code isn't going to sit around making a lot of sleep() calls.
# Instead the more common use is timeouts.  Timeouts are kind of
# interesting though--when a timeout is set, there is typically an
# expectation that it will probably NOT occur.  The expiration of a
# timeout is an exceptional event. Most of the time, a timeout will be
# cancelled before it is allowed to expire.
#
# This presents an interesting implementation challenge for managing
# time.  It is most common to see time managed by sorting the
# expiration times in some way. For example, placing them in a sorted
# list, or ordering them on a heap in a priority queue.  Although
# operations on these kinds of data structures can be managed in O(log N)
# steps, they might not be necessary at all if you make some slightly
# different assumptions about time management.
#
# The queue implementation here is based on the idea that expiration
# times in the distant future don't need to be precisely sorted.
# Instead, you can merely drop expiration times in a dict with the
# hope that they'll be cancelled later.  Manipulating a dict in this
# case is O(1)--meaning that is extremely cheap to setup and teardown
# a timeout that never occurs.  For timeouts in the near future, they
# can still be sorted using a priority queue in the usual way.

import heapq

class TimeQueue:
    cutoff = 1.0         # Threshhold for near/far events (seconds)
    def __init__(self):
        self.near = [ ]
        self.far = { }
        self.near_deadline = 0
        self.far_min_deadline = float('inf')

    def _far_to_near(self):
        '''
        Move items from the far queue to the near queue (if any).
        '''
        removed = []
        min_deadline = float('inf')
        for item, expires in self.far.items():
            if expires < self.near_deadline:
                self.push(item, expires)
                removed.append(item)
            elif expires < min_deadline:
                min_deadline = expires
        for item in removed:
            del self.far[item]
        self.far_min_deadline = min_deadline

    def next_deadline(self, current_clock):
        '''
        Returns the number of seconds to delay until the next deadline
        expires.  current_clock is the current value of the clock.
        Returns None if there are no pending deadlines.
        '''
        self.near_deadline = current_clock + self.cutoff
        if self.near_deadline > self.far_min_deadline:
            self._far_to_near()

        if self.near:
            delta = self.near[0][0] - current_clock
            return delta if delta > 0 else 0

        # There are no near deadlines. Use the closest far deadline
        if self.far:
            delta = self.far_min_deadline - current_clock
            return delta if delta > 0 else 0

        # There are no sleeping tasks of any kind.
        return None

    def push(self, item, expires):
        '''
        Push a new item onto the time queue.
        '''
        # If the expiration time is closer than the current near deadline,
        # it gets pushed onto a heap in order to preserve order
        if expires <= self.near_deadline:
            heapq.heappush(self.near, (expires, item))
        else:
            # Otherwise the item gets put into a dict for far-in-future handling
            if item not in self.far or self.far[item] > expires:
                self.far[item] = expires
            if expires < self.far_min_deadline:
                self.far_min_deadline = expires

    def expired(self, deadline):
        '''
        An iterator that returns all items that have expired up to a given deadline
        '''
        near = self.near
        if deadline >= self.far_min_deadline:
            self.near_deadline = deadline + self.cutoff
            self._far_to_near()

        while near and near[0][0] <= deadline:
            yield  heapq.heappop(near)

    def cancel(self, item, expires):
        '''
        Cancel a time event. The combination of (item, expires) should
        match a prior push() operation (but if not, it's ignored).
        '''
        self.far.pop(item, None)
