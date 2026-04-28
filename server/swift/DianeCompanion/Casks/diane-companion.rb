cask "diane-companion" do
  version "1.0.0"
  sha256 :no_check # Replace with actual SHA256 after first release

  url "https://github.com/emergent-company/diane/releases/download/v#{version}/DianeCompanion-#{version}.dmg"
  name "Diane Companion"
  desc "macOS menu bar app for monitoring your Memory Platform server"
  homepage "https://github.com/emergent-company/diane"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: ">= :ventura"

  app "DianeCompanion.app"

  zap trash: [
    "~/Library/Preferences/com.emergent-company.diane-companion.plist",
    "~/Library/Application Support/DianeCompanion",
  ]
end
