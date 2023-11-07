import React, { useCallback, useEffect, useState } from "react";
import * as R from "ramda";
import styled from "styled-components/macro"; // eslint-disable-line no-unused-vars
import MButton from "@material-ui/core/Button";
import Container from "@material-ui/core/Container";
import * as C from "../Components";
import Select from "./Select";
import TextInput from "./TextInput";
import Slider from "./Slider";
import { countryOptionData, listOptionData } from "./data";
import { parseQueryString } from "utils";
import { useTypeOptions, useProgramOptions } from "./options";
import { saveAs } from "file-saver";

const Button = styled(MButton)`
  margin: 1em;
`;

const ButtonSet = styled.div`
  display: flex;
  justify-content: flex-start;
  && > * {
    margin-right: 1em;
  }
`;

const Cell = styled.div`
  outline: 0px solid #eee;
  display: flex;
  align-items: flex-end;
  margin-bottom: 1em;
`;

const TwoColumns = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-gap: 1em 2em;
`;

const initialValues = {
  address: "",
  name: "",
  altName: "",
  city: "",
  state: "",
  providence: "",
  country: "",
  zip: "",
  limit: 10,
  q: "",
  sdnType: "individual",
  program: "",
  threshold: 99,
  // disabled ///////////
  // idNumber: "",
  // list: "All",
  score: 100
};

// eslint-disable-next-line
export default ({ onSubmit, onReset }) => {
  const [values, setValues] = React.useState(initialValues);

  const { values: typeOptionValues } = useTypeOptions();
  const { values: programOptionValues } = useProgramOptions();

  const handleChange = name => e => {
    const value = R.path(["target", "value"], e);
    setValues(values => R.assoc(name, value, values));
  };

  const handleChangeSlider = name => (e, value) => {
    setValues(values => R.assoc(name, value, values));
  };

  const handleSearchClick = () => {
    const activeValues = R.omit(["idNumber", "list", "score"])(values);
    onSubmit(activeValues);
  };

  const handleResetClick = () => {
    setValues(initialValues);
    onReset();
  };

  const [selectedFile, setSelectedFile] = useState(null);

  const handleFileSelect = (e) => {
    setSelectedFile(e.target.files[0]);
  };

  const handleBatchSearchClick = (e) => {  
    const formData = new FormData();
    if (selectedFile) {
      formData.append("csvFile", selectedFile);
    } else {
      alert("Please select a CSV file!");
      return;
    }
    formData.append("sdnType", values.sdnType);
    formData.append("min-match", values.score / 100);
    formData.append("threshold", values.threshold / 100);

    fetch("/search/batch", {
      method: "POST",
      body: formData
    })
      .then(response => {
        if (response.ok) {
          return response.text();
        } else {
          throw new Error("Request failed with status: " + response.status);
        }
      })
      .then(data => {
        const blob = new Blob([data], { type: "text/csv" });
        const timestamp = new Date().toISOString().replace(/[-:]/g, "");
        const filename = `cc_ofac_search_results_${timestamp}.csv`;
        saveAs(blob, filename);
      })
      .catch(error => {
        console.error("Error:", error);
      });
  };
  
  // eslint-disable-next-line
  const submit = useCallback(onSubmit, []);
  useEffect(() => {
    const { search } = window.location;
    if (!search) {
      return;
    }
    setValues(values => {
      const newValues = R.mergeDeepRight(values, parseQueryString(search));
      submit(newValues);
      return newValues;
    });
  }, [submit]);

  return (
    <Container>
      <form
        onSubmit={e => {
          e.preventDefault();
          handleBatchSearchClick();
        }}
      >
        <C.Section style={{ width: "50%"}}>
          <C.SectionTitle>Batch Search</C.SectionTitle>
          <Cell style={{ paddingTop: "1em", width: "75%" }}>
            <Slider
              label="Minimum Name Score"
              id="score-slider"
              value={values["score"]}
              onChange={handleChangeSlider("score")}
              min={50}
              max={100}
              valueLabelDisplay="auto"
            />
            &nbsp;&nbsp;
            {values["score"]}
          </Cell>
          <Cell style={{ paddingTop: "1em", width: "75%" }}>
            <Slider
              label="Match Threshold"
              id="threshold-slider"
              value={values["threshold"]}
              onChange={handleChangeSlider("threshold")}
              min={50}
              max={100}
              valueLabelDisplay="auto"
            />
            &nbsp;&nbsp;
            {values["threshold"]}
          </Cell>
          <Cell style={{ width: "75%" }}>
            <Select
              label="Type"
              id="sdnType"
              value={values["sdnType"]}
              onChange={handleChange("sdnType")}
              options={typeOptionValues}
            />
          </Cell>
          <Cell style={{ paddingTop: "1em", width: "75%" }}>
            <label>CSV File:</label>
            &nbsp;&nbsp;
            <input type="file" onChange={handleFileSelect} />
          </Cell>
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <Cell>
              <ButtonSet>
                <Button variant="contained" color="primary" type="submit">
                  Search
                </Button>
              </ButtonSet>
            </Cell>
          </div>
        </C.Section>
      </form>
      <br /><br /><br /><br /><br />

      <form
        onSubmit={e => {
          e.preventDefault();
          handleSearchClick();
        }}
      >
        <C.Section>
          <C.SectionTitle>Search</C.SectionTitle>
          <TwoColumns>
            <div>
              <Cell>
                <TextInput
                  label="Name | Alt | Address"
                  id="q"
                  value={values["q"]}
                  onChange={handleChange("q")}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="Name"
                  id="name"
                  value={values["name"]}
                  onChange={handleChange("name")}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="Alt Name"
                  id="altName"
                  value={values["altName"]}
                  onChange={handleChange("altName")}
                />
              </Cell>
              <Cell>
                <Select
                  label="Type"
                  id="sdnType"
                  value={values["sdnType"]}
                  onChange={handleChange("sdnType")}
                  options={typeOptionValues}
                />
              </Cell>
              <Cell>
                <Select
                  label="OFAC Program"
                  id="ofacProgram"
                  value={values["ofacProgram"]}
                  onChange={handleChange("ofacProgram")}
                  options={programOptionValues}
                />
              </Cell>
              <Cell>
                <TextInput
                  type="number"
                  label="Limit"
                  id="limit"
                  value={values["limit"]}
                  onChange={handleChange("limit")}
                />
              </Cell>
            </div>
            <div>
              <Cell>
                <TextInput
                  label="Address"
                  id="address"
                  value={values["address"]}
                  onChange={handleChange("address")}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="City"
                  id="city"
                  value={values["city"]}
                  onChange={handleChange("city")}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="State"
                  id="state"
                  value={values["state"]}
                  onChange={handleChange("state")}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="Providence"
                  id="providence"
                  value={values["providence"]}
                  onChange={handleChange("providence")}
                />
              </Cell>
              <Cell>
                <Select
                  label="Country"
                  id="country"
                  value={values["country"]}
                  onChange={handleChange("country")}
                  options={countryOptionData}
                />
              </Cell>
              <Cell>
                <TextInput
                  label="Postal Code"
                  id="zip"
                  value={values["zip"]}
                  onChange={handleChange("zip")}
                />
              </Cell>
            </div>
          </TwoColumns>
          <Cell>
            <ButtonSet>
              <Button variant="contained" color="primary" type="submit">
                Search
              </Button>
              <Button variant="outlined" color="default" onClick={handleResetClick}>
                Reset
              </Button>
            </ButtonSet>
          </Cell>
          {false && (
            <>
              <Cell>
                <Select
                  disabled={true}
                  label="List"
                  id="list"
                  value={values["list"]}
                  onChange={handleChange("list")}
                  options={listOptionData}
                />
              </Cell>
              <Cell>
                <Slider
                  disabled={true}
                  label="Score"
                  id="score"
                  value={values["score"]}
                  onChange={handleChangeSlider("score")}
                  min={0}
                  max={100}
                  valueLabelDisplay="auto"
                />
              </Cell>
            </>
          )}
        </C.Section>
      </form>
    </Container>
  );
};
