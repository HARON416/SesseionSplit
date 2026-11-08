// Copyright 2026 The SessionSplit Authors
// Use of this source code is governed by a BSD-style license that can be
// found in the Chromium LICENSE file.

#ifndef COMPONENTS_EMBEDDER_SUPPORT_SESSIONSPLIT_PROFILE_CONFIG_H_
#define COMPONENTS_EMBEDDER_SUPPORT_SESSIONSPLIT_PROFILE_CONFIG_H_

#include <array>
#include <cstddef>
#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

#include "base/component_export.h"
#include "base/types/expected.h"

namespace embedder_support {

// Parsed startup data only. Ownership, process propagation, persistence, and
// applying environment settings belong to the browser integration layer.
struct SessionSplitProfileConfig {
  static constexpr int kSchemaVersion = 1;
  static constexpr std::size_t kMaxBytes = 16 * 1024;

  std::array<uint8_t, 32> seed;
  std::vector<std::string> languages;
  std::string timezone;
};

// Errors name the invalid field without copying profile data into diagnostics.
COMPONENT_EXPORT(SESSIONSPLIT_PROFILE_CONFIG)
base::expected<SessionSplitProfileConfig, std::string>
ParseSessionSplitProfileConfig(std::string_view json);

}  // namespace embedder_support

#endif  // COMPONENTS_EMBEDDER_SUPPORT_SESSIONSPLIT_PROFILE_CONFIG_H_
