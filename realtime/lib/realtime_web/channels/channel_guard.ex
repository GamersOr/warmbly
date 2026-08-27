defmodule RealtimeWeb.ChannelGuard do
  @moduledoc """
  The shared gate every channel runs before it does any work in `join/3`.

  Two jobs:

  * Throttle joins. `ws_join` is documented as a per-minute channel-join budget,
    but it was only ever spent on the socket handshake, so an established socket
    could issue unlimited `phx_join`s. Each one costs an authorization query, so
    a client stuck in a rejoin loop turned into a Postgres load generator with
    nothing in the path to stop it.
  * Give every join rejection the `{code, reason}` shape the API reference
    promises, instead of the bare `%{reason: ...}` most channels returned.

  Call `check_join/1` FIRST in `join/3`: refusing before the `Auth` lookup is the
  point, since that lookup is the expensive part.
  """

  alias Realtime.Auth
  alias Realtime.RateLimiter

  require Logger

  @doc """
  Spend one unit of this user's channel-join budget.

  Returns `:ok`, or `{:error, payload}` ready to be returned straight from
  `join/3`. Fails open when Redis is down, like every other limiter here.
  """
  def check_join(socket) do
    user_id = socket.assigns.user_id
    limit = socket.assigns |> Map.get(:rate_limits, %{}) |> Map.get(:limit_ws_join_pm)

    case RateLimiter.check(user_id, :ws_join, limit) do
      {:ok, _remaining} ->
        :ok

      {:error, :rate_limited, retry_after_ms} ->
        Logger.debug("User #{user_id} rate limited on ws_join")

        {:error,
         %{
           code: Auth.error_code(:rate_limited),
           reason: "rate_limited",
           category: "ws_join",
           retry_after_ms: retry_after_ms
         }}
    end
  end

  @doc """
  A join rejection carrying both a numeric `code` and the channel's own reason
  slug. The slug is passed through verbatim so published reasons like
  `"campaign_not_found"` keep their meaning for existing clients.
  """
  def join_error(slug, code_for \\ :forbidden)

  def join_error(slug, code_for) when is_binary(slug) do
    {:error, %{code: Auth.error_code(code_for), reason: slug}}
  end

  def join_error(reason, _code_for) when is_atom(reason) do
    {:error, %{code: Auth.error_code(reason), reason: Auth.error_message(reason)}}
  end
end
