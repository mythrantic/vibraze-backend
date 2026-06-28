# Liquidsoap 2.2.5 radio script
# Asks the Go backend for the next track to play, streams to Icecast.

# Settings
settings.log.level := 3

# Fallback: silence if everything else fails
security = blank()

# Ask the Go backend for the next song file path
def get_next_track() =
  # In Liquidsoap 2.2.x, http.get returns (response_string).{headers, status_code, ...}
  # The string itself IS the body
  let result = http.get("http://backend:3000/api/radio/next-track")
  file_path = string.trim(result)
  log("Next track: #{file_path}")
  if file_path != "" then
    request.create(file_path)
  else
    request.create("")
  end
end

# Create a dynamic source from the Go API
radio_stream = request.dynamic(get_next_track)

# Crossfade between tracks (2 seconds)
radio_stream = crossfade(duration=2.0, radio_stream)

# Fallback chain: dynamic stream -> silence
final_radio = fallback(track_sensitive=false, [radio_stream, security])

# Output to Icecast
output.icecast(
  %mp3(bitrate=192),
  host="icecast",
  port=8000,
  password="radio_source_secret",
  mount="stream.mp3",
  name="Vibraze Radio",
  description="Self-hosted radio powered by Go & SvelteKit",
  final_radio
)
