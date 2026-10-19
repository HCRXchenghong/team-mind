/**
 * Guards asynchronous work that belongs to the currently selected team.
 * A response is usable only while both its request generation and team ID
 * still match. This prevents a slow response from a previous team replacing
 * the current team's page state.
 */
export type TeamRequest = { teamId: string; generation: number };

export function useTeamRequest(getTeamId: () => string) {
  let generation = 0;

  function begin(): TeamRequest {
    return { teamId: getTeamId(), generation: ++generation };
  }

  function invalidate() {
    generation += 1;
  }

  function isCurrent(request: TeamRequest) {
    return request.generation === generation && request.teamId === getTeamId();
  }

  return { begin, invalidate, isCurrent };
}
