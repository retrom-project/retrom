# Retrom mkxp-z position evidence bridge, protocol version 1.
#
# This script is injected through mkxp-z's fixed preload-script mechanism. It
# never changes game state. The browser reads the bounded ASCII record from the
# private libretro system directory after a frame boundary.

module RetromPositionBridge
  PATH = "/System/retrom-position-v1"
  TEMP_PATH = "/System/retrom-position-v1.tmp"

  def self.capture
    return unless defined?($game_map) && $game_map
    return unless defined?($game_player) && $game_player
    return unless defined?($game_variables) && $game_variables

    frame = Graphics.respond_to?(:frame_count) ? Graphics.frame_count : 0
    values = [1, $game_map.map_id, $game_player.x, $game_player.y, $game_variables[1], frame]
    File.open(TEMP_PATH, "wb") { |file| file.write(values.join(",")) }
    File.rename(TEMP_PATH, PATH)
  rescue StandardError
    # Evidence unavailability is reported by the browser-side gate. A game must
    # never fail only because the validation observer could not write its file.
  end
end

module Graphics
  class << self
    unless respond_to?(:retrom_update_without_position_bridge)
      alias_method :retrom_update_without_position_bridge, :update

      def update
        retrom_update_without_position_bridge
        RetromPositionBridge.capture
      end
    end
  end
end
