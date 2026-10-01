import { describe, expect, it } from 'vitest';
import { RateLimitedError, createRoomErrorMessage } from './api';

describe('createRoomErrorMessage', () => {
	it.each([
		[1, 'Try again in 1 second.'],
		[45, 'Try again in 45 seconds.'],
		[60, 'Try again in 1 minute.'],
		[61, 'Try again in 2 minutes.']
	])('rate limited for %ss', (secs, tail) => {
		expect(createRoomErrorMessage(new RateLimitedError(secs))).toBe(
			`You're creating pads too quickly. ${tail}`
		);
	});

	it('falls back to a generic message', () => {
		expect(createRoomErrorMessage(new Error('boom'))).toMatch(/Couldn't create a pad/);
	});
});
